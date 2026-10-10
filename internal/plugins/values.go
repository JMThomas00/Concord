package plugins

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// maxTextValue caps text/secret field values.
const maxTextValue = 4000

// ValidateValues checks submitted form values against a manifest's field
// declarations and returns a field-key → problem map (empty when valid).
// submitted holds only the fields being changed; stored is what's saved
// already, used to decide whether a required field ends up empty (a secret
// left untouched keeps its stored value). isTextChannel reports whether a
// channel_select value names a real text channel.
func ValidateValues(fields []ConfigField, submitted, stored map[string]string, isTextChannel func(uuid.UUID) bool) map[string]string {
	problems := map[string]string{}
	declared := map[string]ConfigField{}
	for _, f := range fields {
		declared[f.Key] = f
	}
	for key := range submitted {
		if _, ok := declared[key]; !ok {
			problems[key] = "not a setting this plugin has"
		}
	}

	for _, f := range fields {
		v, changed := submitted[f.Key]
		if !changed {
			v = stored[f.Key]
		}
		if v == "" {
			if f.Required {
				problems[f.Key] = "required"
			}
			continue
		}
		if !changed {
			continue // already saved; only check what's being changed
		}
		switch f.Type {
		case "number":
			if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
				problems[f.Key] = "must be a number"
			}
		case "boolean":
			if v != "true" && v != "false" {
				problems[f.Key] = "must be true or false"
			}
		case "select":
			ok := false
			for _, o := range f.Options {
				ok = ok || o == v
			}
			if !ok {
				problems[f.Key] = "must be one of: " + strings.Join(f.Options, ", ")
			}
		case "channel_select":
			id, err := uuid.Parse(v)
			if err != nil || isTextChannel == nil || !isTextChannel(id) {
				problems[f.Key] = "pick an existing text channel"
			}
		case "channel_multi_select":
			for _, part := range strings.Split(v, ",") {
				id, err := uuid.Parse(strings.TrimSpace(part))
				if err != nil || isTextChannel == nil || !isTextChannel(id) {
					problems[f.Key] = "pick existing text channels"
					break
				}
			}
		case "text", "secret":
			if len(v) > maxTextValue {
				problems[f.Key] = "too long"
			}
		}
	}
	return problems
}
