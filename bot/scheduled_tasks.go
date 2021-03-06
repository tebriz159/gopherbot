package bot

import (
	"fmt"
	"sync"

	"github.com/robfig/cron"
)

var taskRunner *cron.Cron
var schedMutex sync.Mutex

func scheduleTasks() {
	schedMutex.Lock()
	if taskRunner != nil {
		taskRunner.Stop()
	}
	robot.RLock()
	scheduled := robot.scheduledTasks
	tz := robot.timeZone
	robot.RUnlock()
	if tz != nil {
		Log(Info, fmt.Sprintf("Scheduling tasks in TimeZone: %s", tz))
		taskRunner = cron.NewWithLocation(tz)
	} else {
		Log(Info, "Scheduling tasks in system default timezone")
		taskRunner = cron.New()
	}
	currentTasks.RLock()
	tasks := taskList{
		currentTasks.t,
		currentTasks.nameMap,
		currentTasks.idMap,
		currentTasks.nameSpaces,
		sync.RWMutex{},
	}
	currentTasks.RUnlock()
	for _, st := range scheduled {
		t := tasks.getTaskByName(st.Name)
		if t == nil {
			Log(Error, fmt.Sprintf("Task not found when scheduling task: %s", st.Name))
			continue
		}
		task, _, _ := getTask(t)
		if task.Disabled {
			Log(Error, fmt.Sprintf("Not scheduling disabled task '%s'; reason: %s", st.Name, task.reason))
			continue
		}
		Log(Info, fmt.Sprintf("Scheduling job '%s' with schedule: %s", st.Name, st.Schedule))
		taskRunner.AddFunc(st.Schedule, func() { runScheduledTask(t, st.taskSpec, tasks) })
	}
	taskRunner.Start()
	schedMutex.Unlock()
}

func runScheduledTask(t interface{}, ts taskSpec, tasks taskList) {
	task, plugin, _ := getTask(t)
	isPlugin := plugin != nil
	if isPlugin && len(ts.Command) == 0 {
		Log(Error, fmt.Sprintf("Empty 'Command' when running scheduled task '%s' of type plugin", ts.Name))
		return
	}

	// Create the botContext to carry state through the pipeline.
	// runPipeline will take care of registerActive()
	bot := &botContext{
		User:                 task.User,
		Channel:              task.Channel,
		tasks:                tasks,
		isCommand:            isPlugin,
		directMsg:            false,
		bypassSecurityChecks: true, // scheduled jobs don't get authorization / elevation checks
		environment:          make(map[string]string),
	}
	var command string
	if isPlugin {
		command = ts.Command
	} else {
		command = "run"
		for _, p := range ts.Parameters {
			bot.environment[p.Name] = p.Value
		}
	}
	Log(Debug, fmt.Sprintf("Starting scheduled task: %s", task.name))
	bot.runPipeline(t, false, scheduled, command, ts.Arguments...)
}
