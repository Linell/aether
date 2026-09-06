package contract

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type eventDef struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Fields []string `json:"fields"`
}

type eventsFile struct {
	Events map[string]eventDef `json:"events"`
}

func loadContract(t *testing.T) eventsFile {
	t.Helper()
	data, err := os.ReadFile("../../contract/events.json")
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var f eventsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	if len(f.Events) == 0 {
		t.Fatal("contract has no events; check the path or the file contents")
	}
	return f
}

func TestEveryEventHasConstant(t *testing.T) {
	f := loadContract(t)

	for name := range f.Events {
		if _, ok := Payloads[name]; !ok {
			t.Errorf("contract event %q has no Go constant/payload registered", name)
		}
	}
	for name := range Payloads {
		if _, ok := f.Events[name]; !ok {
			t.Errorf("Go event %q is not present in contract/events.json", name)
		}
	}
}

func TestPayloadFieldsMatchContract(t *testing.T) {
	f := loadContract(t)

	for name, def := range f.Events {
		payload, ok := Payloads[name]
		if !ok {
			continue
		}

		want := append([]string(nil), def.Fields...)
		sort.Strings(want)

		got := jsonFieldNames(payload)
		sort.Strings(got)

		if !reflect.DeepEqual(want, got) {
			t.Errorf("event %q: payload json fields %v do not match contract fields %v", name, got, want)
		}
	}
}

func jsonFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}
