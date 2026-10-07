package config

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/riidii-md/vitryna-publish/export"
	"go.yaml.in/yaml/v3"
)

const MaxBytes = 1 << 20
const MaxDepth = 32

type Config struct {
	Version int
	Export  export.Policy
	GitHub  *GitHub
}

type GitHub struct {
	Repository          string `yaml:"repository"`
	BaseBranch          string `yaml:"base_branch"`
	PromotionBranch     string `yaml:"promotion_branch"`
	TrustedSourceBranch string `yaml:"trusted_source_branch"`
	Merge               Merge  `yaml:"merge"`
}

type Merge struct {
	Mode                 string `yaml:"mode"`
	Method               string `yaml:"method"`
	ProtectionInspection string `yaml:"protection_inspection"`
}

func (c Config) ExportPolicy() export.Policy { return c.Export }

type shape struct {
	tag    string
	fields map[string]shape
	item   *shape
}

func Decode(data []byte) (Config, error) {
	if len(data) > MaxBytes {
		return Config{}, invalid("config_too_large", "")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc, extra yaml.Node
	if err := decoder.Decode(&doc); err != nil || len(doc.Content) != 1 {
		return Config{}, invalid("config_yaml", "")
	}
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, invalid("config_multiple_documents", "")
	}
	str := shape{tag: "!!str"}
	list := shape{tag: "!!seq", item: &str}
	schema := shape{tag: "!!map", fields: map[string]shape{
		"version": {tag: "!!int"},
		"export":  {tag: "!!map", fields: map[string]shape{"public": list, "private": list, "forbidden_content": list}},
		"publish": {tag: "!!map", fields: map[string]shape{"github": {tag: "!!map", fields: map[string]shape{
			"repository": str, "base_branch": str, "promotion_branch": str, "trusted_source_branch": str,
			"merge": {tag: "!!map", fields: map[string]shape{"mode": str, "method": str, "protection_inspection": str}},
		}}}},
	}}
	if err := validate(doc.Content[0], schema, "", 0); err != nil {
		return Config{}, err
	}
	var wire struct {
		Version int `yaml:"version"`
		Export  struct {
			Public  []string `yaml:"public"`
			Private []string `yaml:"private"`
			Markers []string `yaml:"forbidden_content"`
		} `yaml:"export"`
		Publish *struct {
			GitHub *GitHub `yaml:"github"`
		} `yaml:"publish"`
	}
	if err := doc.Decode(&wire); err != nil {
		return Config{}, invalid("config_type", "")
	}
	c := Config{Version: wire.Version, Export: export.Policy{PublicPaths: wire.Export.Public, PrivatePaths: wire.Export.Private, ForbiddenContent: wire.Export.Markers}}
	if c.Version != 1 {
		return Config{}, invalid("config_version", "version")
	}
	if err := c.Export.Validate(); err != nil {
		return Config{}, err
	}
	if wire.Publish != nil {
		if wire.Publish.GitHub == nil {
			return Config{}, invalid("config_target_missing", "publish.github")
		}
		c.GitHub = wire.Publish.GitHub
		if err := c.GitHub.validate(); err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

func validate(n *yaml.Node, s shape, path string, depth int) error {
	if depth > MaxDepth || n.Kind == yaml.AliasNode || n.Anchor != "" {
		return invalid("config_structure", path)
	}
	if n.Tag != s.tag {
		return invalid("config_type", path)
	}
	switch s.tag {
	case "!!map":
		if n.Kind != yaml.MappingNode {
			return invalid("config_type", path)
		}
		seen := make(map[string]bool)
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Anchor != "" {
				return invalid("config_key", path)
			}
			field, ok := s.fields[key.Value]
			if !ok {
				return invalid("config_unknown_field", path)
			}
			if seen[key.Value] {
				return invalid("config_duplicate_key", path)
			}
			seen[key.Value] = true
			childPath := key.Value
			if path != "" {
				childPath = path + "." + key.Value
			}
			if err := validate(n.Content[i+1], field, childPath, depth+1); err != nil {
				return err
			}
		}
	case "!!seq":
		if n.Kind != yaml.SequenceNode {
			return invalid("config_type", path)
		}
		for _, child := range n.Content {
			if err := validate(child, *s.item, path, depth+1); err != nil {
				return err
			}
		}
	default:
		if n.Kind != yaml.ScalarNode {
			return invalid("config_type", path)
		}
	}
	return nil
}

func invalid(code, path string) error { return &export.Diagnostic{Code: code, Path: path} }

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

func (g *GitHub) validate() error {
	if !repositoryPattern.MatchString(g.Repository) || strings.HasSuffix(g.Repository, "/.") || strings.HasSuffix(g.Repository, "/..") {
		return invalid("config_repository", "publish.github.repository")
	}
	for field, ref := range map[string]string{"base_branch": g.BaseBranch, "promotion_branch": g.PromotionBranch, "trusted_source_branch": g.TrustedSourceBranch} {
		if !safeBranch(ref) {
			return invalid("config_branch", "publish.github."+field)
		}
	}
	if g.BaseBranch == g.PromotionBranch {
		return invalid("config_reserved_branch", "publish.github.promotion_branch")
	}
	if g.Merge.Mode == "" {
		g.Merge.Mode = "manual"
	}
	switch g.Merge.Mode {
	case "manual":
		if g.Merge.Method != "" || g.Merge.ProtectionInspection != "" {
			return invalid("config_manual_auto_fields", "publish.github.merge")
		}
	case "auto":
		if g.Merge.Method != "squash" && g.Merge.Method != "merge" && g.Merge.Method != "rebase" {
			return invalid("config_merge_method", "publish.github.merge.method")
		}
		if g.Merge.ProtectionInspection != "effective_rulesets" && g.Merge.ProtectionInspection != "classic" {
			return invalid("config_protection_mode", "publish.github.merge.protection_inspection")
		}
	default:
		return invalid("config_merge_mode", "publish.github.merge.mode")
	}
	return nil
}

func safeBranch(ref string) bool {
	if ref == "" || ref == "@" || ref == "HEAD" || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.HasSuffix(ref, ".") {
		return false
	}
	for _, r := range ref {
		if r <= 32 || r == 127 || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
