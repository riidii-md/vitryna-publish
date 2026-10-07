package config_test

import (
	"fmt"

	"github.com/riidii-md/vitryna-publish/config"
)

func ExampleDecode() {
	cfg, err := config.Decode([]byte("version: 1\nexport:\n  public: [README.md]\n  private: [docs/private]\n"))
	if err != nil {
		panic(err)
	}
	policy := cfg.ExportPolicy()
	fmt.Println(cfg.Version, policy.PublicPaths[0], cfg.GitHub == nil)
	// Output: 1 README.md true
}
