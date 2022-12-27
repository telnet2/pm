package pm

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct {
	time.Duration
}

// UnmarshalJSON helps to decode a string-type duration.
func (duration *Duration) UnmarshalJSON(b []byte) error {
	var unmarshalledJson interface{}

	err := json.Unmarshal(b, &unmarshalledJson)
	if err != nil {
		return err
	}

	switch value := unmarshalledJson.(type) {
	case float64:
		duration.Duration = time.Duration(value)
	case string:
		duration.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid duration: %#v", unmarshalledJson)
	}

	return nil
}

// UnmarshalYAML helps to decode a string-type duration.
func (duration *Duration) UnmarshalYAML(value *yaml.Node) error {
	return value.Decode(&duration.Duration)
}

// GoFunc starts a given `fun` in a new go-routine and waits until the go-routine starts.
func GoFunc(fun func()) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		wg.Done()
		fun()
	}()
	wg.Wait()
}
