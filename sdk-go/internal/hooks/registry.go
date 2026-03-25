package hooks

import (
	"context"
	"fmt"
	"sync"
)

type Registry struct {
	mu         sync.Mutex
	callbacks  map[string]Callback
	nextID     int
	configured bool
	config     map[Event][]Matcher
	encoded    map[string]any
}

func NewRegistry(config map[Event][]Matcher) *Registry {
	cloned := make(map[Event][]Matcher, len(config))
	for event, matchers := range config {
		cloned[event] = append([]Matcher(nil), matchers...)
	}
	return &Registry{
		callbacks: make(map[string]Callback),
		config:    cloned,
	}
}

func (r *Registry) InitializeConfig() map[string]any {
	if r == nil || len(r.config) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	result := make(map[string]any, len(r.config))
	if r.configured {
		for event, value := range r.encoded {
			result[event] = cloneMatcherConfigs(value)
		}
		return result
	}

	for event, matchers := range r.config {
		items := make([]map[string]any, 0, len(matchers))
		for _, matcher := range matchers {
			callbackIDs := make([]string, 0, len(matcher.Hooks))
			for _, callback := range matcher.Hooks {
				id := fmt.Sprintf("hook_%d", r.nextID)
				r.nextID++
				r.callbacks[id] = callback
				callbackIDs = append(callbackIDs, id)
			}

			item := map[string]any{
				"hookCallbackIds": callbackIDs,
			}
			if matcher.Matcher != nil {
				item["matcher"] = *matcher.Matcher
			} else {
				item["matcher"] = nil
			}
			if matcher.Timeout > 0 {
				item["timeout"] = matcher.Timeout.Seconds()
			}
			items = append(items, item)
		}
		result[string(event)] = items
	}

	r.encoded = make(map[string]any, len(result))
	for key, value := range result {
		r.encoded[key] = cloneMatcherConfigs(value)
	}
	r.configured = true
	return result
}

func cloneMatcherConfigs(value any) any {
	items, ok := value.([]map[string]any)
	if !ok {
		return value
	}
	cloned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := make(map[string]any, len(item))
		for key, raw := range item {
			switch typed := raw.(type) {
			case []string:
				entry[key] = append([]string(nil), typed...)
			default:
				entry[key] = typed
			}
		}
		cloned = append(cloned, entry)
	}
	return cloned
}

func (r *Registry) Handle(ctx context.Context, callbackID string, input map[string]any, toolUseID *string) (map[string]any, error) {
	if r == nil {
		return nil, fmt.Errorf("no hook callbacks configured")
	}

	r.mu.Lock()
	callback, ok := r.callbacks[callbackID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no hook callback found for ID: %s", callbackID)
	}

	result, err := callback(ctx, input, toolUseID, Context{Signal: nil})
	if err != nil {
		return nil, err
	}
	return EncodeResult(result)
}
