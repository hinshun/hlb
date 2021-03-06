package solver

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/containerd/console"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/sshforward/sshprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
)

type SolveOption func(*SolveInfo) error

type SolveInfo struct {
	LogOutput          LogOutput
	OutputDockerRef    string
	OutputWriter       io.WriteCloser
	OutputPushImage    string
	OutputLocal        string
	OutputLocalTarball bool
	Locals             map[string]string
}

type LogOutput int

const (
	LogOutputTTY LogOutput = iota
	LogOutputPlain
	LogOutputJSON
	LogOutputRaw
)

func WithLogOutput(logOutput LogOutput) SolveOption {
	return func(info *SolveInfo) error {
		info.LogOutput = logOutput
		return nil
	}
}

func WithDownloadDockerTarball(ref string, w io.WriteCloser) SolveOption {
	return func(info *SolveInfo) error {
		info.OutputDockerRef = ref
		info.OutputWriter = w
		return nil
	}
}

func WithPushImage(ref string) SolveOption {
	return func(info *SolveInfo) error {
		info.OutputPushImage = ref
		return nil
	}
}

func WithDownload(dest string) SolveOption {
	return func(info *SolveInfo) error {
		info.OutputLocal = dest
		return nil
	}
}

func WithDownloadTarball(w io.WriteCloser) SolveOption {
	return func(info *SolveInfo) error {
		info.OutputLocalTarball = true
		info.OutputWriter = w
		return nil
	}
}

func WithLocal(id, path string) SolveOption {
	return func(info *SolveInfo) error {
		info.Locals[id] = path
		return nil
	}
}

func Solve(ctx context.Context, c *client.Client, st llb.State, opts ...SolveOption) error {
	info := SolveInfo{
		Locals: make(map[string]string),
	}
	for _, opt := range opts {
		err := opt(&info)
		if err != nil {
			return err
		}
	}

	def, err := st.Marshal(llb.LinuxAmd64)
	if err != nil {
		return err
	}

	attachable := []session.Attachable{authprovider.NewDockerAuthProvider(os.Stderr)}

	if _, set := os.LookupEnv("SSH_AUTH_SOCK"); set {
		cfg := sshprovider.AgentConfig{
			ID: "default",
		}

		sp, err := sshprovider.NewSSHAgentProvider([]sshprovider.AgentConfig{cfg})
		if err != nil {
			return err
		}
		attachable = append(attachable, sp)
	}

	wrapWriter := func(wc io.WriteCloser) func(map[string]string) (io.WriteCloser, error) {
		return func(m map[string]string) (io.WriteCloser, error) {
			return wc, nil
		}
	}

	solveOpt := client.SolveOpt{
		Session:   attachable,
		LocalDirs: make(map[string]string),
	}

	if info.OutputDockerRef != "" {
		solveOpt.Exports = append(solveOpt.Exports, client.ExportEntry{
			Type: client.ExporterDocker,
			Attrs: map[string]string{
				"name": info.OutputDockerRef,
			},
			Output: wrapWriter(info.OutputWriter),
		})
	}

	if info.OutputPushImage != "" {
		solveOpt.Exports = append(solveOpt.Exports, client.ExportEntry{
			Type: client.ExporterImage,
			Attrs: map[string]string{
				"name": info.OutputPushImage,
				"push": "true",
			},
		})
	}

	if info.OutputLocal != "" {
		solveOpt.Exports = append(solveOpt.Exports, client.ExportEntry{
			Type:      client.ExporterLocal,
			OutputDir: info.OutputLocal,
		})
	}

	if info.OutputLocalTarball {
		solveOpt.Exports = append(solveOpt.Exports, client.ExportEntry{
			Type:   client.ExporterTar,
			Output: wrapWriter(info.OutputWriter),
		})
	}

	for id, path := range info.Locals {
		solveOpt.LocalDirs[id] = path
	}

	ch := make(chan *client.SolveStatus)
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		_, err := c.Build(ctx, solveOpt, "", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			res, err := c.Solve(ctx, gateway.SolveRequest{
				Definition: def.ToPB(),
			})
			if err != nil {
				return nil, err
			}

			if _, ok := res.Metadata[exptypes.ExporterImageConfigKey]; !ok {
				img := specs.Image{
					Config: specs.ImageConfig{
						Env:        st.Env(),
						Entrypoint: st.GetArgs(),
						WorkingDir: st.GetDir(),
					},
				}

				config, err := json.Marshal(img)
				if err != nil {
					return nil, err
				}

				res.AddMeta(exptypes.ExporterImageConfigKey, config)
			}
			return res, nil
		}, ch)
		return err
	})

	eg.Go(func() error {
		switch info.LogOutput {
		case LogOutputTTY, LogOutputPlain:
			var c console.Console
			if info.LogOutput == LogOutputTTY {
				var err error
				c, err = console.ConsoleFromFile(os.Stderr)
				if err != nil {
					return err
				}
			}

			// not using shared context to not disrupt display but let is finish reporting errors
			return progressui.DisplaySolveStatus(context.TODO(), "", c, os.Stderr, ch)
		case LogOutputJSON, LogOutputRaw:
			return StreamSolveStatus(ctx, info.LogOutput, os.Stdout, ch)
		}
		return nil
	})

	err = eg.Wait()
	if err != nil {
		return err
	}

	return nil
}
