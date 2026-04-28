package query

import "sync"

var commandQueue = struct {
	sync.Mutex
	items    []QueuedCommand
	listener CommandLifecycleListener
}{}

// EnqueueCommand queues a command to be injected into the next query loop.
func EnqueueCommand(command QueuedCommand) {
	commandQueue.Lock()
	defer commandQueue.Unlock()
	commandQueue.items = append(commandQueue.items, command)
}

// ClearQueuedCommands clears pending commands. It is primarily useful for tests.
func ClearQueuedCommands() {
	commandQueue.Lock()
	defer commandQueue.Unlock()
	commandQueue.items = nil
}

// SetCommandLifecycleListener installs a process-wide lifecycle listener.
func SetCommandLifecycleListener(listener CommandLifecycleListener) {
	commandQueue.Lock()
	defer commandQueue.Unlock()
	commandQueue.listener = listener
}

func drainQueuedCommands() []QueuedCommand {
	commandQueue.Lock()
	defer commandQueue.Unlock()
	items := append([]QueuedCommand(nil), commandQueue.items...)
	commandQueue.items = nil
	return items
}

func notifyQueuedCommand(uuid, event string) {
	commandQueue.Lock()
	listener := commandQueue.listener
	commandQueue.Unlock()
	if listener != nil {
		listener(uuid, event)
	}
}
