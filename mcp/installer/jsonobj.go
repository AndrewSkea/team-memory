package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// object is a JSON object that preserves key order, so editing a user's
// settings file doesn't reshuffle it.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func newObject() *object { return &object{vals: map[string]json.RawMessage{}} }

func parseObject(data []byte) (*object, error) {
	o := newObject()
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte{0xEF, 0xBB, 0xBF})
	if len(data) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("expected a JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o.set(k, v)
	}
	return o, nil
}

func (o *object) get(k string) (json.RawMessage, bool) {
	v, ok := o.vals[k]
	return v, ok
}

func (o *object) set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) setValue(k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	o.set(k, b)
	return nil
}

func (o *object) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// child returns a nested object (creating an empty one if absent).
func (o *object) child(k string) (*object, error) {
	v, ok := o.get(k)
	if !ok || string(v) == "null" {
		return newObject(), nil
	}
	c, err := parseObject(v)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", k, err)
	}
	return c, nil
}

func (o *object) setChild(k string, c *object) {
	if len(c.keys) == 0 {
		o.del(k)
		return
	}
	b, _ := c.MarshalJSON()
	o.set(k, b)
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func readObjectFile(path string) (*object, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newObject(), nil
	}
	if err != nil {
		return nil, err
	}
	o, err := parseObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v) — fix or remove it and re-run", path, err)
	}
	return o, nil
}

// writeObjectFile writes atomically with 2-space indentation, keeping a
// one-time backup of the original.
func writeObjectFile(path string, o *object) error {
	raw, err := o.MarshalJSON()
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		backup := path + ".team-memory-backup"
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if orig, err := os.ReadFile(path); err == nil {
				_ = os.WriteFile(backup, orig, 0o600)
			}
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
