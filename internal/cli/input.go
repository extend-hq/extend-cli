package cli

import (
	"context"
	"errors"
	"fmt"
	"sync"

	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// uploadProgressMu serializes writes to ErrOut when uploadOrResolveWith is
// called from multiple goroutines (batch uploads). Without it, concurrent
// fmt.Fprintf calls would race on a *bytes.Buffer in tests and produce
// interleaved progress lines on stderr in production.
var uploadProgressMu sync.Mutex

func uploadOrResolve(ctx context.Context, app *App, cli *sdkclient.Client, input string) (extendx.FileRef, error) {
	return uploadOrResolveWith(ctx, app, cli, input, "")
}

// resolveInputOrText turns either a positional input (file path, file_xxx ID,
// or https:// URL) or an inline --text value into a FileRef. Exactly one of
// (input, text) must be set. --name sets a display name for --text and URL
// inputs (uploaded files and file IDs carry their own name). --password is
// honored only for URL inputs (see uploadOrResolveWith). Used by the verbs
// whose SDK file union accepts inline text: extract, classify, run.
func resolveInputOrText(ctx context.Context, app *App, cli *sdkclient.Client, input, text, name, password string) (extendx.FileRef, error) {
	if text != "" {
		if input != "" {
			return extendx.FileRef{}, errors.New("provide either an input argument or --text, not both")
		}
		if password != "" {
			return extendx.FileRef{}, errors.New("--password does not apply to --text inputs")
		}
		return extendx.FileRef{Text: text, Name: name}, nil
	}
	if input == "" {
		return extendx.FileRef{}, errors.New("provide an input (a file path, file_xxx ID, or https:// URL) or use --text")
	}
	ref, err := uploadOrResolveWith(ctx, app, cli, input, password)
	if err != nil {
		return extendx.FileRef{}, err
	}
	if name != "" {
		if ref.URL == "" {
			return extendx.FileRef{}, errors.New("--name applies only to URL or --text inputs")
		}
		ref.Name = name
	}
	return ref, nil
}

// uploadOrResolveWith is the password-aware variant of uploadOrResolve.
//
// When password != "":
//   - URL inputs get settings.password set on the FileRef
//   - All other inputs (local upload, file_id, text) error out, since the
//     server only honors settings.password on URL inputs (FileFromUrlSchema
//     is the sole schema with a settings field). Silently dropping the
//     password would leave the user wondering why a password-protected PDF
//     fails to parse.
func uploadOrResolveWith(ctx context.Context, app *App, cli *sdkclient.Client, input, password string) (extendx.FileRef, error) {
	ref, localPath, err := extendx.ResolveInput(input)
	if err != nil {
		return extendx.FileRef{}, err
	}
	if localPath == "-" {
		return extendx.FileRef{}, errors.New("stdin (-) is not supported; save the input to a file first (the file extension determines content-type server-side)")
	}
	if password != "" && (localPath != "" || ref.ID != "") {
		return extendx.FileRef{}, errors.New("--password is only honored for URL inputs; the API has no way to attach a password to uploaded files or file IDs (decrypt the PDF locally first if you need to upload)")
	}
	if localPath != "" {
		printUploadProgress(app, "Uploading %s...\n", localPath)
		f, err := extendx.UploadFile(ctx, cli, localPath)
		if err != nil {
			return extendx.FileRef{}, fmt.Errorf("upload: %w", err)
		}
		printUploadProgress(app, "Uploaded as %s\n", f.ID)
		ref = extendx.FileRef{ID: f.ID}
	}
	if password != "" && ref.URL != "" {
		ref.Settings = &extendx.FileSettings{Password: password}
	}
	return ref, nil
}

// printUploadProgress writes a single progress line to app.IO.ErrOut under
// uploadProgressMu so concurrent batch workers don't garble each other.
func printUploadProgress(app *App, format string, args ...any) {
	uploadProgressMu.Lock()
	defer uploadProgressMu.Unlock()
	fmt.Fprintf(app.IO.ErrOut, format, args...)
}
