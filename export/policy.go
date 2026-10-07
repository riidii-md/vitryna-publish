package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riidii-md/vitryna-publish/internal/portablepath"
)

var controlFiles = [...]string{".vitryna.yml", ".github/workflows/vitryna-publish.yml"}

type Policy struct {
	PublicPaths      []string `json:"public"`
	PrivatePaths     []string `json:"private"`
	ForbiddenContent []string `json:"forbiddenContent"`
}

type Diagnostic struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
	Rule string `json:"rule,omitempty"`
}

func (d *Diagnostic) Error() string {
	if d.Path != "" {
		return fmt.Sprintf("%s: %s", d.Code, d.Path)
	}
	return d.Code
}

func (p Policy) Validate() error {
	_, err := p.compile()
	return err
}

type compiledPolicy struct {
	public, private ruleIndex
	publicPaths     []string
}

func (p Policy) compile() (compiledPolicy, error) {
	compiled := compiledPolicy{publicPaths: p.PublicPaths}
	if len(p.PublicPaths) == 0 || len(p.PrivatePaths) == 0 {
		return compiled, &Diagnostic{Code: "policy_empty_rules"}
	}
	for i, list := range [][]string{p.PublicPaths, p.PrivatePaths} {
		index := &compiled.public
		if i == 1 {
			index = &compiled.private
		}
		for _, rule := range list {
			if !safeRule(rule) {
				return compiled, &Diagnostic{Code: "policy_unsafe_rule", Path: rule}
			}
			if other := index.overlap(rule); other != "" {
				return compiled, &Diagnostic{Code: "policy_redundant_rule", Path: rule, Rule: other}
			}
			index.add(rule)
		}
	}
	for _, public := range p.PublicPaths {
		if err := portablepath.Validate(public); err != nil {
			return compiled, &Diagnostic{Code: "public_path_invalid", Path: public}
		}
		if private := compiled.private.overlap(public); private != "" {
			return compiled, &Diagnostic{Code: "policy_visibility_overlap", Path: public, Rule: private}
		}
		for _, control := range controlFiles {
			if contains(public, control) {
				return compiled, &Diagnostic{Code: "policy_public_control", Path: public}
			}
		}
	}
	seen := make(map[string]bool)
	for _, marker := range p.ForbiddenContent {
		if marker == "" || !utf8.ValidString(marker) || seen[marker] {
			return compiled, &Diagnostic{Code: "policy_invalid_marker"}
		}
		seen[marker] = true
	}
	return compiled, nil
}

func safeRule(p string) bool {
	if p == "" || !utf8.ValidString(p) || strings.ContainsRune(p, '\\') {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, c := range strings.Split(p, "/") {
		if c == "" || c == "." || c == ".." {
			return false
		}
	}
	return true
}

func contains(rule, p string) bool { return p == rule || strings.HasPrefix(p, rule+"/") }

func (p compiledPolicy) classify(path string) (visibility, rule string) {
	for _, control := range controlFiles {
		if path == control {
			return "private", control
		}
	}
	if rule := p.private.match(path); rule != "" {
		return "private", rule
	}
	if rule := p.public.match(path); rule != "" {
		return "public", rule
	}
	return "", ""
}

func (p Policy) Digest() string {
	copySorted := func(xs []string) []string { ys := append([]string{}, xs...); sort.Strings(ys); return ys }
	p = Policy{copySorted(p.PublicPaths), copySorted(p.PrivatePaths), copySorted(p.ForbiddenContent)}
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
