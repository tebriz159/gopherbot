package bot

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// MessageFormat indicates how the connector should display the content of
// the message. One of Variable, Fixed or Raw
type MessageFormat int

// Outgoing message format, Variable or Fixed
const (
	Raw MessageFormat = iota // protocol native, zero value -> default if not specified
	Fixed
	Variable
)

// Connector protocols
type Protocol int

const (
	Slack Protocol = iota
	Terminal
	Test
)

// Robot is passed to each task as it runs, initialized from the botContext.
// Tasks can copy and modify the Robot without affecting the botContext.
type Robot struct {
	User     string        // The user who sent the message; this can be modified for replying to an arbitrary user
	Channel  string        // The channel where the message was received, or "" for a direct message. This can be modified to send a message to an arbitrary channel.
	Protocol Protocol      // slack, terminal, test, others; used for interpreting rawmsg or sending messages with Format = 'Raw'
	RawMsg   interface{}   // raw struct of message sent by connector; interpret based on protocol. For Slack this is a *slack.MessageEvent
	Format   MessageFormat // The outgoing message format, one of Raw, Fixed, or Variable
	id       int           // For looking up the botContext
}

//go:generate stringer -type=Protocol

// Generate String method with: go generate ./bot/

/* robot_methods.go defines some convenience functions on struct Robot to
   simplify use by plugins. */

// getContext returns the botContext for a given Robot
func (r *Robot) getContext() *botContext {
	return getBotContextInt(r.id)
}

// CheckAdmin returns true if the user is a configured administrator of the
// robot. Should be used sparingly, when a single plugin has multiple commands,
// some which require admin. Otherwise the plugin should just configure
// RequireAdmin: true
func (r *Robot) CheckAdmin() bool {
	robot.RLock()
	defer robot.RUnlock()
	for _, adminUser := range robot.adminUsers {
		if r.User == adminUser {
			emit(AdminCheckPassed)
			return true
		}
	}
	emit(AdminCheckFailed)
	return false
}

// SetParameter sets a parameter for the current pipeline, useful only for
// passing parameters (as environment variables) to tasks later in the pipeline.
// StoreParameter is for long-term parameter storage (e.g. credentials).
func (r *Robot) SetParameter(name, value string) bool {
	if !identifierRe.MatchString(name) {
		return false
	}
	c := r.getContext()
	c.environment[name] = value
	return true
}

// AddTask puts another task (job or plugin) in the queue for the pipeline. Unlike other
// CI/CD tools, gopherbot pipelines are code generated, not configured; it is,
// however, trivial to write code that reads an arbitrary configuration file
// and uses AddTask to generate a pipeline. When the task is a plugin, cmdargs
// should be a command followed by arguments. For jobs, only the name is
// required; parameters should be specified in calls to SetParameter.
func (r *Robot) AddTask(name string, cmdargs ...string) RetVal {
	c := r.getContext()
	t := c.tasks.getTaskByName(name)
	if t == nil {
		return TaskNotFound
	}
	_, plugin, _ := getTask(t)
	isPlugin := plugin != nil
	var command string
	var args []string
	if isPlugin {
		if len(cmdargs) == 0 {
			return MissingArguments
		}
		if len(cmdargs[0]) == 0 {
			return MissingArguments
		}
		command, args = cmdargs[0], cmdargs[1:]
	} else {
		command = "run"
		args = []string{}
	}
	ts := taskSpec{
		Name:      name,
		Command:   command,
		Arguments: args,
		task:      t,
	}
	c.nextTasks = append(c.nextTasks, ts)
	return Ok
}

// GetParameter retrieves the value of a parameter for a namespace. Only useful
// for Go plugins; external scripts have all parameters for the NameSpace stored
// as environment variables. Note that runtasks.go populates the environment
// with Stored paramters, too. So GetParameter is useful for both short-term
// parameters in a pipeline, and for getting long-term parameters such as
// credentials.
func (r *Robot) GetParameter(key string) string {
	c := r.getContext()
	value, ok := c.environment[key]
	if ok {
		return value
	}
	return ""
}

// Elevate lets a plugin request elevation on the fly. When immediate = true,
// the elevator should always prompt for 2fa; otherwise a configured timeout
// should apply.
func (r *Robot) Elevate(immediate bool) bool {
	c := r.getContext()
	task, _, _ := getTask(c.currentTask)
	retval := c.elevate(task, immediate)
	if retval == Success {
		return true
	}
	return false
}

// Fixed is a deprecated convenience function for sending a message with fixed width
// font.
func (r *Robot) Fixed() *Robot {
	nr := *r
	nr.Format = Fixed
	return &nr
}

// MessageFormat returns a robot object with the given format, most likely for a
// plugin that will mostly use e.g. Variable format.
func (r *Robot) MessageFormat(f MessageFormat) *Robot {
	r.Format = f
	return r
}

// Direct is a convenience function for initiating a DM conversation with a
// user. Created initially so a plugin could prompt for a password in a DM.
func (r *Robot) Direct() *Robot {
	nr := *r
	nr.Channel = ""
	return &nr
}

// Pause is a convenience function to pause some fractional number of seconds.
func (r *Robot) Pause(s float64) {
	ms := time.Duration(s * float64(1000))
	time.Sleep(ms * time.Millisecond)
}

// RandomString is a convenience function for returning a random string
// from a slice of strings, so that replies can vary.
func (r *Robot) RandomString(s []string) string {
	l := len(s)
	if l == 0 {
		return ""
	}
	return s[random.Intn(l)]
}

// RandomInt uses the robot's seeded random to return a random int 0 <= retval < n
func (r *Robot) RandomInt(n int) int {
	return random.Intn(n)
}

// GetBotAttribute returns an attribute of the robot or "" if unknown.
// Current attributes:
// name, alias, fullName, contact
func (r *Robot) GetBotAttribute(a string) *AttrRet {
	a = strings.ToLower(a)
	robot.RLock()
	defer robot.RUnlock()
	ret := Ok
	var attr string
	switch a {
	case "name":
		attr = robot.name
	case "fullname", "realname":
		attr = robot.fullName
	case "alias":
		attr = string(robot.alias)
	case "email":
		attr = robot.email
	case "contact", "admin", "admincontact":
		attr = robot.adminContact
	case "protocol":
		attr = r.Protocol.String()
	default:
		ret = AttributeNotFound
	}
	return &AttrRet{attr, ret}
}

// GetUserAttribute returns a AttrRet with
// - The string Attribute of a user, or "" if unknown/error
// - A RetVal which is one of Ok, UserNotFound, AttributeNotFound
// Current attributes:
// name(handle), fullName, email, firstName, lastName, phone, internalID
// TODO: supplement data with gopherbot.json user's table
func (r *Robot) GetUserAttribute(u, a string) *AttrRet {
	a = strings.ToLower(a)
	attr, ret := robot.GetProtocolUserAttribute(u, a)
	return &AttrRet{attr, ret}
}

// messageHeard sends a typing notification
func (r *Robot) messageHeard() {
	robot.MessageHeard(r.User, r.Channel)
}

// GetSenderAttribute returns a AttrRet with
// - The string Attribute of the sender, or "" if unknown/error
// - A RetVal which is one of Ok, UserNotFound, AttributeNotFound
// Current attributes:
// name(handle), fullName, email, firstName, lastName, phone, internalID
// TODO: supplement data with gopherbot.json user's table
func (r *Robot) GetSenderAttribute(a string) *AttrRet {
	a = strings.ToLower(a)
	switch a {
	case "name", "username", "handle", "user", "user name":
		return &AttrRet{r.User, Ok}
	default:
		attr, ret := robot.GetProtocolUserAttribute(r.User, a)
		return &AttrRet{attr, ret}
	}
}

/*

GetTaskConfig sets a struct pointer to point to a config struct populated
from configuration when plugins were loaded. To use, a plugin should define
a struct for it's configuration data, e.g.:

	type pConf struct {
		Username, Password string
	}

In conf/plugins/<pluginname>.yaml, you would add a Config: stanza, e.g.:

	Config:
	  Username: foo
	  Password: bar

When registering the plugin, you pass a pointer to an empty config template, which the
robot will use to populate a struct when configuration is loaded:

	func init() {
		bot.RegisterPlugin("memes", bot.PluginHandler{
			DefaultConfig: defaultConfig, // yaml string providing default configuration
			Handler:       plugfunc, // callback function
			Config:        &pConf{}, // pointer to empty config struct
		})
	}

Then, to get a current copy of configuration when the plugin runs, define a struct pointer
and call GetTaskConfig with a double-pointer:

	var c *pConf
	r.GetTaskConfig(&c)

... And voila! *pConf is populated with the contents from the configured Config: stanza
*/
func (r *Robot) GetTaskConfig(dptr interface{}) RetVal {
	c := r.getContext()
	task, _, _ := getTask(c.currentTask)
	if task.config == nil {
		Log(Debug, fmt.Sprintf("Task \"%s\" called GetTaskConfig, but no config was found.", task.name))
		return NoConfigFound
	}
	tp := reflect.ValueOf(dptr)
	if tp.Kind() != reflect.Ptr {
		Log(Debug, fmt.Sprintf("Task \"%s\" called GetTaskConfig, but didn't pass a double-pointer to a struct", task.name))
		return InvalidDblPtr
	}
	p := reflect.Indirect(tp)
	if p.Kind() != reflect.Ptr {
		Log(Debug, fmt.Sprintf("Task \"%s\" called GetTaskConfig, but didn't pass a double-pointer to a struct", task.name))
		return InvalidDblPtr
	}
	if p.Type() != reflect.ValueOf(task.config).Type() {
		Log(Debug, fmt.Sprintf("Task \"%s\" called GetTaskConfig with an invalid double-pointer", task.name))
		return InvalidCfgStruct
	}
	p.Set(reflect.ValueOf(task.config))
	return Ok
}

// Log logs a message to the robot's log file (or stderr) if the level
// is lower than or equal to the robot's current log level
func (r *Robot) Log(l LogLevel, v ...interface{}) {
	c := r.getContext()
	if c.logger != nil {
		c.logger.Log("LOG:" + logLevelToStr(l) + " " + fmt.Sprintln(v...))
	}
	Log(l, v...)
}

// SendChannelMessage lets a plugin easily send a message to an arbitrary
// channel. Use Robot.Fixed().SendChannelMessage(...) for fixed-width
// font.
func (r *Robot) SendChannelMessage(channel, msg string) RetVal {
	return robot.SendProtocolChannelMessage(channel, msg, r.Format)
}

// SendUserChannelMessage lets a plugin easily send a message directed to
// a specific user in a specific channel without fiddling with the robot
// object. Use Robot.Fixed().SencChannelMessage(...) for fixed-width
// font.
func (r *Robot) SendUserChannelMessage(user, channel, msg string) RetVal {
	return robot.SendProtocolUserChannelMessage(user, channel, msg, r.Format)
}

// SendUserMessage lets a plugin easily send a DM to a user. If a DM
// isn't possible, the connector should message the user in a channel.
func (r *Robot) SendUserMessage(user, msg string) RetVal {
	return robot.SendProtocolUserMessage(user, msg, r.Format)
}

// Reply directs a message to the user
func (r *Robot) Reply(msg string) RetVal {
	if r.Channel == "" {
		return robot.SendProtocolUserMessage(r.User, msg, r.Format)
	}
	return robot.SendProtocolUserChannelMessage(r.User, r.Channel, msg, r.Format)
}

// Say just sends a message to the user or channel
func (r *Robot) Say(msg string) RetVal {
	if r.Channel == "" {
		return robot.SendProtocolUserMessage(r.User, msg, r.Format)
	}
	return robot.SendProtocolChannelMessage(r.Channel, msg, r.Format)
}
