package bot

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
)

// PluginNames can be letters, numbers & underscores only, mainly so
// brain functions can use ':' as a separator.
var identifierRe = regexp.MustCompile(`[\w-]+`)

// Global persistent map of plugin name to unique ID
var taskNameIDmap = struct {
	m map[string]string
	sync.Mutex
}{
	make(map[string]string),
	sync.Mutex{},
}

type taskList struct {
	t          []interface{}
	nameMap    map[string]int
	idMap      map[string]int
	nameSpaces map[string]struct{}
	sync.RWMutex
}

var currentTasks = &taskList{
	nil,
	nil,
	nil,
	nil,
	sync.RWMutex{},
}

func getTask(t interface{}) (*botTask, *botPlugin, *botJob) {
	p, ok := t.(*botPlugin)
	if ok {
		return p.botTask, p, nil
	} else {
		j := t.(*botJob)
		return j.botTask, nil, j
	}
}

func (tl *taskList) getTaskByName(name string) interface{} {
	tl.RLock()
	ti, ok := tl.nameMap[name]
	if !ok {
		Log(Error, fmt.Sprintf("Task '%s' not found calling getTaskByName", name))
		tl.RUnlock()
		return nil
	}
	task := tl.t[ti]
	tl.RUnlock()
	return task
}

func (tl *taskList) getTaskByID(id string) interface{} {
	tl.RLock()
	ti, ok := tl.idMap[id]
	if !ok {
		Log(Error, fmt.Sprintf("Task '%s' not found calling getTaskByID", id))
		tl.RUnlock()
		return nil
	}
	task := tl.t[ti]
	tl.RUnlock()
	return task
}

func getPlugin(t interface{}) *botPlugin {
	p, ok := t.(*botPlugin)
	if ok {
		return p
	}
	return nil
}

func getJob(t interface{}) *botJob {
	j, ok := t.(*botJob)
	if ok {
		return j
	}
	return nil
}

// Struct for ScheduledTasks (gopherbot.yaml) and AddTask (robot method)
type taskSpec struct {
	Name      string // name of the job or plugin
	Command   string // plugins only
	Arguments []string
	// environment vars for jobs and plugins, unused in AddTask, which should
	// make calls to SetParameter()
	Parameters []parameter
	task       interface{} // populated in AddTask
}

// parameters are provided to jobs and plugins as environment variables
type parameter struct {
	Name, Value string
}

type externalPlugin struct {
	// List of names, paths and types for external plugins and jobs; relative paths are searched first in installpath, then configpath
	Name, Path string
}

type externalJob struct {
	// List of names, paths and types for external plugins and jobs; relative paths are searched first in installpath, then configpath
	Name, Description string
}

// items in gopherbot.yaml
type scheduledTask struct {
	Schedule string // timespec for https://godoc.org/github.com/robfig/cron
	taskSpec
}

// PluginHelp specifies keywords and help text for the 'bot help system
type PluginHelp struct {
	Keywords []string // match words for 'help XXX'
	Helptext []string // help string to give for the keywords, conventionally starting with (bot) for commands or (hear) when the bot needn't be addressed directly
}

// Indicates what started the pipeline
type pipelineType int

const (
	plugCommand pipelineType = iota
	plugMessage
	catchAll
	jobTrigger
	scheduled
	runJob
)

// InputMatcher specifies the command or message to match for a plugin, or user and message to trigger a job
type InputMatcher struct {
	Regex      string         // The regular expression string to match - bot adds ^\w* & \w*$
	Command    string         // The name of the command to pass to the plugin with it's arguments
	Label      string         // ReplyMatchers use "Label" instead of "Command"
	Contexts   []string       // label the contexts corresponding to capture groups, for supporting "it" & optional args
	User       string         // jobs only; user that can trigger this job, normally git-activated webhook or integration
	Parameters []string       // jobs only; names of parameters (environment vars) where regex matches are stored, in order of capture groups
	re         *regexp.Regexp // The compiled regular expression. If the regex doesn't compile, the 'bot will log an error
}

type taskType int

const (
	taskGo taskType = iota
	taskExternal
)

// a botTask can be a plugin or a job, both capable of calling Robot methods.
type botTask struct {
	name             string          // name of job or plugin; unique by type, but job & plugin can share
	taskType         taskType        // taskGo or taskExternal
	Path             string          // Path to the external executable for jobs or Plugtype=taskExternal only
	NameSpace        string          // callers that share namespace share long-term memories and environment vars; defaults to name if not otherwise set
	PrivateNameSpace bool            // when set for tasks, memories will be stored/retrieved from task namespace instead of pipeline
	Description      string          // description of job or plugin
	HistoryLogs      int             // how many runs of this job/plugin to keep history for
	AllowDirect      bool            // Set this true if this plugin can be accessed via direct message
	DirectOnly       bool            // Set this true if this plugin ONLY accepts direct messages
	Channel          string          // channel where a job can be interracted with, channel where a scheduled task (job or plugin) runs
	Channels         []string        // plugins only; Channels where the plugin is available - rifraf like "memes" should probably only be in random, but it's configurable. If empty uses DefaultChannels
	AllChannels      bool            // If the Channels list is empty and AllChannels is true, the plugin should be active in all the channels the bot is in
	User             string          // for scheduled tasks (jobs or plugins), task runs as this user, also for notifies
	RequireAdmin     bool            // Set to only allow administrators to access a plugin
	Users            []string        // If non-empty, list of all the users with access to this plugin
	Elevator         string          // Use an elevator other than the DefaultElevator
	Authorizer       string          // a plugin to call for authorizing users, should handle groups, etc.
	AuthRequire      string          // an optional group/role name to be passed to the Authorizer plugin, for group/role-based authorization determination
	taskID           string          // 32-char random ID for identifying plugins/jobs
	ReplyMatchers    []InputMatcher  // store this here for prompt*reply methods
	Config           json.RawMessage // Arbitrary Plugin configuration, will be stored and provided in a thread-safe manner via GetTaskConfig()
	config           interface{}     // A pointer to an empty struct that the bot can Unmarshal custom configuration into
	Disabled         bool
	reason           string // why this job/plugin is disabled
}

// stuff read in conf/jobs/<job>.yaml
type botJob struct {
	Verbose            bool           // whether to send verbose "job started/ended" messages
	Triggers           []InputMatcher // user/regex that triggers a job, e.g. a git-activated webhook or integration
	Parameters         []parameter    // Fixed parameters for a given job; many jobs will use the same script with differing parameters
	RequiredParameters []string       // required in schedule, prompted to user for interactive
	*botTask
}

// Plugin specifies the structure of a plugin configuration - plugins should include an example / default config
type botPlugin struct {
	AdminCommands            []string       // A list of commands only a bot admin can use
	ElevatedCommands         []string       // Commands that require elevation, usually via 2fa
	ElevateImmediateCommands []string       // Commands that always require elevation promting, regardless of timeouts
	AuthorizedCommands       []string       // Which commands to authorize
	AuthorizeAllCommands     bool           // when ALL commands need to be authorized
	Help                     []PluginHelp   // All the keyword sets / help texts for this plugin
	CommandMatchers          []InputMatcher // Input matchers for messages that need to be directed to the 'bot
	MessageMatchers          []InputMatcher // Input matchers for messages the 'bot hears even when it's not being spoken to
	CatchAll                 bool           // Whenever the robot is spoken to, but no plugin matches, plugins with CatchAll=true get called with command="catchall" and argument=<full text of message to robot>
	*botTask
}

// PluginHandler is the struct a plugin registers for the Gopherbot plugin API.
type PluginHandler struct {
	DefaultConfig string /* A yaml-formatted multiline string defining the default Plugin configuration. It should be liberally commented for use in generating
	custom configuration for the plugin. If a Config: section is defined, it should match the structure of the optional Config interface{} */
	Handler func(bot *Robot, command string, args ...string) TaskRetVal // The callback function called by the robot whenever a Command is matched
	Config  interface{}                                                 // An optional empty struct defining custom configuration for the plugin
}

var pluginHandlers = make(map[string]PluginHandler)

// stopRegistrations is set "true" when the bot is created to prevent registration outside of init functions
var stopRegistrations = false

// initialize sends the "init" command to every plugin
func initializePlugins() {
	currentTasks.RLock()
	tasks := taskList{
		currentTasks.t,
		currentTasks.nameMap,
		currentTasks.idMap,
		currentTasks.nameSpaces,
		sync.RWMutex{},
	}
	currentTasks.RUnlock()
	bot := &botContext{
		environment: make(map[string]string),
		tasks:       tasks,
	}
	bot.registerActive()
	robot.Lock()
	if !robot.shuttingDown {
		robot.Unlock()
		for _, t := range tasks.t {
			task, plugin, _ := getTask(t)
			if plugin == nil {
				continue
			}
			if task.Disabled {
				continue
			}
			Log(Info, "Initializing plugin:", task.name)
			bot.callTask(t, "init")
		}
	} else {
		robot.Unlock()
	}
	bot.deregister()
}

// Update passed-in regex so that a space can match a variable # of spaces,
// to prevent cut-n-paste spacing related non-matches.
func massageRegexp(r string) string {
	replaceSpaceRe := regexp.MustCompile(`\[([^]]*) ([^]]*)\]`)
	regex := replaceSpaceRe.ReplaceAllString(r, `[$1\x20$2]`)
	regex = strings.Replace(regex, " ?", `\s*`, -1)
	regex = strings.Replace(regex, " ", `\s+`, -1)
	Log(Trace, fmt.Sprintf("Updated regex '%s' => '%s'", r, regex))
	return regex
}

// RegisterPlugin allows Go plugins to register a PluginHandler in a func init().
// When the bot initializes, it will call each plugin's handler with a command
// "init", empty channel, the bot's username, and no arguments, so the plugin
// can store this information for, e.g., scheduled jobs.
// See builtins.go for the pluginHandlers definition.
func RegisterPlugin(name string, plug PluginHandler) {
	if stopRegistrations {
		return
	}
	if !identifierRe.MatchString(name) {
		log.Fatalf("Plugin name '%s' doesn't match plugin name regex '%s'", name, identifierRe.String())
	}
	if _, exists := pluginHandlers[name]; exists {
		log.Fatalf("Attempted plugin name registration duplicates builtIn or other Go plugin: %s", name)
	}
	pluginHandlers[name] = plug
}

func getTaskID(plug string) string {
	taskNameIDmap.Lock()
	taskID, ok := taskNameIDmap.m[plug]
	if ok {
		taskNameIDmap.Unlock()
		return taskID
	}
	// Generate a random id
	p := make([]byte, 16)
	rand.Read(p)
	taskID = fmt.Sprintf("%x", p)
	taskNameIDmap.m[plug] = taskID
	taskNameIDmap.Unlock()
	return taskID
}
