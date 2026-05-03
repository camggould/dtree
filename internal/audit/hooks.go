package audit

import (
	"sync"

	"github.com/cgould/dtree/internal/core"
)

// Hook is a callback invoked synchronously after a successful Append.
// Hooks must not panic; their return value is ignored. Hooks run in
// registration order while a package-internal mutex is held — keep them
// fast or hand off to a goroutine.
type Hook func(core.Event)

var (
	hooksMu sync.RWMutex
	hooks   []Hook
)

// RegisterHook adds h to the hook list. Returns an unregister function.
func RegisterHook(h Hook) (unregister func()) {
	hooksMu.Lock()
	hooks = append(hooks, h)
	idx := len(hooks) - 1
	hooksMu.Unlock()

	return func() {
		hooksMu.Lock()
		defer hooksMu.Unlock()
		if idx < len(hooks) {
			hooks[idx] = nil
		}
	}
}

func notifyHooks(ev core.Event) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	for _, h := range hooks {
		if h != nil {
			h(ev)
		}
	}
}
