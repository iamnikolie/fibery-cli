package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// clipboardPNGScript writes the clipboard's image (coerced to PNG) to the file
// at the given path. It runs under osascript. If the clipboard holds no image
// the coercion fails and osascript exits non-zero, which the caller surfaces as
// "no image on clipboard". %q is the destination path (a controlled temp file,
// so no AppleScript escaping hazards).
const clipboardPNGScript = `set f to open for access (POSIX file %q) with write permission
set eof f to 0
try
	write (the clipboard as «class PNGf») to f
	close access f
on error errMsg
	try
		close access f
	end try
	error errMsg
end try`

// grabClipboardImage returns the current clipboard contents as PNG bytes. It is
// macOS-only (uses osascript); other platforms return an error. When the
// clipboard holds no image it returns a "no image on clipboard" error.
func grabClipboardImage(ctx context.Context) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("--clipboard is only supported on macOS")
	}

	tmp, err := os.CreateTemp("", "fibery-clipboard-*.png")
	if err != nil {
		return nil, fmt.Errorf("clipboard: create temp file: %w", err)
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "osascript", "-e", fmt.Sprintf(clipboardPNGScript, path))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("no image on clipboard")
		}
		return nil, fmt.Errorf("no image on clipboard (%s)", msg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("clipboard: read temp file: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("no image on clipboard")
	}
	return data, nil
}
