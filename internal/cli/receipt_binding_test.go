package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/artifact"
)

func TestWorkflowOriginalReceiptBinding(t *testing.T) {
	for _, scenario := range []string{"original", "tree only", "receipt only", "tree and receipt"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			tree, receiptPath := filepath.Join(root, "tree"), filepath.Join(root, "receipt.json")
			var out, errs bytes.Buffer
			if exit := Run(ctx, []string{"export", "--repository", inspectFixture(t), "--output", tree, "--receipt", receiptPath}, &out, &errs, "test"); exit != 0 {
				t.Fatalf("export: %d %s", exit, errs.String())
			}
			original, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(original)
			binding := hex.EncodeToString(hash[:])
			bundle := filepath.Join(root, "seal")
			if err := artifact.Seal(ctx, tree, receiptPath, bundle); err != nil {
				t.Fatal(err)
			}
			sealed, err := os.ReadFile(filepath.Join(bundle, "tree.tar"))
			if err != nil {
				t.Fatal(err)
			}
			var receipt export.Receipt
			if err := json.Unmarshal(original, &receipt); err != nil {
				t.Fatal(err)
			}
			if scenario == "tree only" || scenario == "tree and receipt" {
				body := []byte("replacement public bytes\n")
				if err := os.WriteFile(filepath.Join(tree, "README.md"), body, 0644); err != nil {
					t.Fatal(err)
				}
				for i := range receipt.Files {
					if receipt.Files[i].Path == "README.md" {
						receipt.TotalBytes += int64(len(body)) - receipt.Files[i].Size
						receipt.Files[i].Size = int64(len(body))
						digest := sha256.Sum256(body)
						receipt.Files[i].SHA256 = hex.EncodeToString(digest[:])
					}
				}
				compact, err := json.Marshal(receipt.Files)
				if err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(append([]byte("vitryna-tree-v1\n"), compact...))
				receipt.TreeSHA256 = hex.EncodeToString(digest[:])
			}
			if scenario == "receipt only" {
				receipt.ToolVersion = "changed provenance"
			}
			if scenario == "receipt only" || scenario == "tree and receipt" {
				data, err := json.MarshalIndent(receipt, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(receiptPath, append(data, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := artifact.VerifyBound(ctx, tree, receiptPath, ""); err != nil {
					t.Fatalf("replacement pair is not internally valid: %v", err)
				}
			}
			out.Reset()
			errs.Reset()
			args := []string{"workflow", "verify-artifact", "--tree", tree, "--receipt", receiptPath, "--receipt-sha256", binding}
			exit := Run(ctx, args, &out, &errs, "test")
			if scenario == "original" {
				if exit != 0 || errs.Len() != 0 || out.Len() == 0 {
					t.Fatalf("original binding rejected: %d %s", exit, errs.String())
				}
			} else if exit != 1 || out.Len() != 0 {
				t.Fatalf("mutation accepted or wrong refusal: %d %s", exit, errs.String())
			}
			if scenario == "receipt only" || scenario == "tree and receipt" {
				previous := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = previous })
				requests := 0
				http.DefaultTransport = controlTransport(func(*http.Request) (*http.Response, error) {
					requests++
					return nil, errors.New("unexpected preview request")
				})
				out.Reset()
				errs.Reset()
				args = []string{"workflow", "compare", "--tree", tree, "--receipt", receiptPath, "--receipt-sha256", binding, "--target", "example/public", "--base", "main", "--promotion", "vitryna/public"}
				if exit := Run(ctx, args, &out, &errs, "test"); exit != 1 || out.Len() != 0 || requests != 0 {
					t.Fatalf("preview used replacement receipt: exit=%d requests=%d %s", exit, requests, errs.String())
				}
			}
			current, err := os.ReadFile(filepath.Join(bundle, "tree.tar"))
			if err != nil || !bytes.Equal(current, sealed) {
				t.Fatal("verification changed original seal")
			}
		})
	}
}
