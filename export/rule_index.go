package export

import "strings"

type ruleIndex struct {
	children map[string]*ruleIndex
	rule     string
	first    string
}

func (n *ruleIndex) add(rule string) {
	for rest := rule; ; {
		if n.first == "" {
			n.first = rule
		}
		part, next, more := strings.Cut(rest, "/")
		if n.children == nil {
			n.children = make(map[string]*ruleIndex)
		}
		if n.children[part] == nil {
			n.children[part] = &ruleIndex{}
		}
		n = n.children[part]
		if !more {
			n.rule, n.first = rule, rule
			return
		}
		rest = next
	}
}

func (n *ruleIndex) overlap(path string) string {
	for rest := path; ; {
		if n.rule != "" {
			return n.rule
		}
		part, next, more := strings.Cut(rest, "/")
		n = n.children[part]
		if n == nil {
			return ""
		}
		if !more {
			// Validated prior rules are disjoint; preserve the first input-order descendant.
			return n.first
		}
		rest = next
	}
}

func (n *ruleIndex) match(path string) string {
	for rest := path; ; {
		part, next, more := strings.Cut(rest, "/")
		n = n.children[part]
		if n == nil {
			return ""
		}
		if n.rule != "" || !more {
			return n.rule
		}
		rest = next
	}
}
