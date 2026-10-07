package config

import (
	"strings"
	"testing"
)

const local = "version: 1\nexport:\n  public: [README.md]\n  private: [secret]\n"
const target = "publish:\n  github:\n    repository: riidii-md/example\n    base_branch: main\n    promotion_branch: vitryna/promote\n    trusted_source_branch: main\n"

func TestDecodeDefaultsAndProjection(t *testing.T) {
	c, err := Decode([]byte(local + target))
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHub == nil || c.GitHub.Merge.Mode != "manual" || c.ExportPolicy().PublicPaths[0] != "README.md" {
		t.Fatalf("bad projection/default: %#v", c)
	}
	if c, err = Decode([]byte(local)); err != nil || c.GitHub != nil {
		t.Fatalf("local config: %#v %v", c, err)
	}
	_, err = Decode([]byte(local + target + "    merge:\n      mode: auto\n      method: squash\n      protection_inspection: effective_rulesets\n"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestStrictDecode(t *testing.T) {
	bad := []string{
		"", "[]", "null", local + "---\n", local + "---\n" + local,
		strings.Replace(local, "version: 1", "version: '1'", 1),
		strings.Replace(local, "version: 1", "version: 2", 1),
		strings.Replace(local, "version: 1", "version: 1\nversion: 1", 1),
		local + "oops: true\n", strings.Replace(local, "public:", "publci:", 1),
		strings.Replace(local, "README.md", "42", 1),
		strings.Replace(local, "README.md", "true", 1),
		strings.Replace(local, "[README.md]", "null", 1),
		strings.Replace(local, "[README.md]", "!custom [README.md]", 1),
		strings.Replace(local, "[README.md]", "&pub [README.md]", 1),
		local + "copy: *pub\n", local + "export:\n  <<: {public: [a]}\n",
		local + "publish: {}\n", local + "publish: null\n", local + "publish:\n  github: {}\n",
		strings.Replace(local+target, "promotion_branch: vitryna/promote", "promotion_branch: main", 1),
		strings.Replace(local+target, "trusted_source_branch: main", "trusted_source_branch: ../main", 1),
		strings.Replace(local+target, "repository: riidii-md/example", "repository: https://token@example.com", 1),
		local + target + "    typo: value\n",
		local + target + "    merge:\n      method: squash\n",
		local + target + "    merge:\n      mode: auto\n",
		local + target + "    merge:\n      mode: auto\n      method: delete\n      protection_inspection: classic\n",
		local + target + "    merge:\n      mode: manual\n      method: squash\n",
		local + "export_secret: hidden-value\n",
		strings.Repeat("#", (1<<20)+1),
	}
	for i, data := range bad {
		if _, err := Decode([]byte(data)); err == nil {
			t.Errorf("invalid document %d accepted", i)
		} else if strings.Contains(err.Error(), "hidden-value") {
			t.Fatal("raw YAML value leaked")
		}
	}
}
