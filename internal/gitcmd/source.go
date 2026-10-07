package gitcmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var ErrSourceRead = errors.New("source_git_read_failed: required objects must be local; prefetch outside Vitryna and retry")
var ErrUnsupportedGit = errors.New("source_git_unsupported: Git >=2.45 with --no-lazy-fetch is required")

func command(ctx context.Context, repository string, args ...string) *exec.Cmd {
	argv := []string{"--no-replace-objects", "--no-lazy-fetch"}
	if repository != "" {
		argv = append(argv, "-C", repository)
	}
	cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
	cmd.Env = baseEnvironment()
	cmd.Stderr = io.Discard
	return cmd
}

// Git needs runtime/locale settings, not the control job's App key or API tokens.
func baseEnvironment() []string {
	var result []string
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		switch name {
		case "PATH", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "SystemRoot", "WINDIR", "COMSPEC", "PATHEXT":
			result = append(result, variable)
		}
	}
	return append(result, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
}

func Capability(ctx context.Context) error {
	data, err := Read(ctx, "", 1024, "--version")
	if err != nil {
		return ErrUnsupportedGit
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return ErrUnsupportedGit
	}
	parts := strings.Split(fields[2], ".")
	if len(parts) < 2 {
		return ErrUnsupportedGit
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || major < 2 || major == 2 && minor < 45 {
		return ErrUnsupportedGit
	}
	return nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	left   int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.left {
		return 0, ErrSourceRead
	}
	n, err := b.buffer.Write(p)
	b.left -= int64(n)
	return n, err
}

func Read(ctx context.Context, repository string, maxBytes int64, args ...string) ([]byte, error) {
	if maxBytes < 0 {
		return nil, ErrSourceRead
	}
	buffer := limitedBuffer{left: maxBytes}
	cmd := command(ctx, repository, args...)
	cmd.Stdout = &buffer
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrSourceRead
	}
	return buffer.buffer.Bytes(), nil
}

type boundedWriter struct {
	destination   io.Writer
	left, written int64
	err           error
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.left {
		w.err = ErrSourceRead
		return 0, w.err
	}
	n, err := w.destination.Write(p)
	w.left -= int64(n)
	w.written += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func Copy(ctx context.Context, repository string, destination io.Writer, maxBytes int64, args ...string) (int64, error) {
	if maxBytes < 0 {
		return 0, ErrSourceRead
	}
	sink := boundedWriter{destination: destination, left: maxBytes}
	cmd := command(ctx, repository, args...)
	cmd.Stdout = &sink
	err := cmd.Run()
	if sink.err != nil {
		return sink.written, sink.err
	}
	if ctx.Err() != nil {
		return sink.written, ctx.Err()
	}
	if err != nil {
		return sink.written, ErrSourceRead
	}
	return sink.written, nil
}

func Records(ctx context.Context, repository string, visit func(string) error, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	childContext, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := command(childContext, repository, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return ErrSourceRead
	}
	if err := cmd.Start(); err != nil {
		return ErrSourceRead
	}
	reader := bufio.NewReader(pipe)
	for {
		record, readErr := reader.ReadString(0)
		if readErr == io.EOF && record == "" {
			break
		}
		if readErr != nil {
			cancel()
			_ = cmd.Wait()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrSourceRead
		}
		if err := visit(strings.TrimSuffix(record, "\x00")); err != nil {
			cancel()
			_ = cmd.Wait()
			return err
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrSourceRead
	}
	return nil
}
