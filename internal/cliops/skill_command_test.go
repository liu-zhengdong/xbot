package cliops

import "testing"

func TestSkillRuntimeParser(t *testing.T) {
	t.Run("read flags surround literal id", func(t *testing.T) {
		c, err := ParseSkillRuntime([]string{"read", "--json", "Legacy Name", "--self"})
		if err != nil || c.Action != "read" || c.ID != "Legacy Name" || !c.Self || !c.JSON || c.Body != nil {
			t.Fatalf("command=%+v error=%v", c, err)
		}
	})
	t.Run("explicit empty body and scope", func(t *testing.T) {
		c, err := ParseSkillRuntime([]string{"write", "--self", "ship", "--description", "release", "--body", "", "--scope=", "--title=发布"})
		if err != nil || c.Slug != "ship" || c.Body == nil || *c.Body != "" || c.Scope != "" || c.Title != "发布" {
			t.Fatalf("command=%+v error=%v", c, err)
		}
	})
	t.Run("scope stays literal", func(t *testing.T) {
		for _, scope := range []string{"global", "BoB", " self "} {
			c, err := ParseSkillRuntime([]string{"write", "ship", "--description=d", "--body-file=-", "--scope", scope, "--self"})
			if err != nil || c.Scope != scope || c.BodyFile != "-" || c.Body != nil {
				t.Fatalf("scope=%q command=%+v error=%v", scope, c, err)
			}
		}
	})
	t.Run("help needs no operation inputs", func(t *testing.T) {
		for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {"read", "--help"}, {"write", "-h"}} {
			if c, err := ParseSkillRuntime(args); err != nil || !c.Help {
				t.Fatalf("argv=%q command=%+v error=%v", args, c, err)
			}
		}
	})
	t.Run("reject unsupported or ambiguous syntax", func(t *testing.T) {
		for _, args := range [][]string{
			nil,
			{"show", "ship"},
			{"read", "ship"},
			{"read", "--self"},
			{"read", "", "--self"},
			{"read", "ship", "other", "--self"},
			{"read", "ship", "--self", "--body", "body"},
			{"read", "ship", "--self", "--scope", ""},
			{"read", "ship", "--self", "--self"},
			{"read", "ship", "--self=true"},
			{"read", "ship", "--self", "--http", "127.0.0.1:9"},
			{"read", "ship", "--self", "--bot", "ada"},
			{"read", "ship", "--self", "--unknown"},
			{"read", "--help", "--unknown"},
			{"read", "--help", "-h"},
			{"write", "ship", "--self", "--body", ""},
			{"write", "ship", "--self", "--description", "d"},
			{"write", "ship", "--self", "--description", "d", "--body-file", ""},
			{"write", "ship", "--self", "--description", "d", "--body", "", "--body-file", "-"},
			{"write", "ship", "--self", "--description", "d", "--body", "", "--body=x"},
			{"write", "ship", "--self", "--description", "d", "--description=d", "--body", ""},
			{"write", "ship", "--self", "--description", "d", "--body", "", "--scope=", "--scope=self"},
			{"write", "ship", "--self", "--description", "d", "--body"},
			{"help", "--unknown"},
		} {
			if c, err := ParseSkillRuntime(args); err == nil {
				t.Errorf("accepted argv=%q command=%+v", args, c)
			}
		}
	})
}
