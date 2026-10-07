package portablepath

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const MaxBytes = 1024

func Validate(p string) error {
	if len(p) == 0 || len(p) > MaxBytes || !utf8.ValidString(p) || !norm.NFC.IsNormalString(p) {
		return fmt.Errorf("invalid public path")
	}
	for _, r := range p {
		if unicode.IsControl(r) || strings.ContainsRune("\\<>:\"|?*", r) {
			return fmt.Errorf("invalid public path character")
		}
	}
	for _, component := range strings.Split(p, "/") {
		if component == "" || component == "." || component == ".." || strings.EqualFold(component, ".git") || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return fmt.Errorf("invalid public path component")
		}
		base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || device(base, "COM") || device(base, "LPT") {
			return fmt.Errorf("reserved public path component")
		}
	}
	return nil
}

func device(base, prefix string) bool {
	if !strings.HasPrefix(base, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(base, prefix)
	return len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' || suffix == "¹" || suffix == "²" || suffix == "³"
}

type entry struct {
	path string
	file bool
}
type Set struct{ paths map[string]entry }

func NewSet() *Set { return &Set{paths: make(map[string]entry)} }

func (s *Set) Add(p string) error {
	if err := Validate(p); err != nil {
		return err
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		key := norm.NFC.String(cases.Fold().String(prefix))
		file := i == len(parts)-1
		if previous, ok := s.paths[key]; ok {
			if previous.path != prefix || previous.file || file {
				return fmt.Errorf("public path collision")
			}
		} else {
			s.paths[key] = entry{path: prefix, file: file}
		}
	}
	return nil
}
