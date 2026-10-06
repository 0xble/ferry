package ops

import (
	"context"
	"fmt"
	"io"

	"github.com/0xble/toolkit/op"
)

type DoctorInput struct{}

// DoctorReport is the health of Tailscale and the daemon. Its fields are in
// the alphabetical order the old map output had.
type DoctorReport struct {
	DaemonError    string `json:"daemon_error,omitempty"`
	DaemonOK       bool   `json:"daemon_ok"`
	TailscaleError string `json:"tailscale_error,omitempty"`
	TailscaleOK    bool   `json:"tailscale_ok"`
}

func registerDoctor(reg *op.Registry, b *Backend) {
	op.Add(reg, op.Op[DoctorInput, DoctorReport]{
		Name: "doctor", Summary: "Check daemon and Tailscale health", Effect: op.Read, MCP: true,
		Handler: func(ctx context.Context, req op.Request, in DoctorInput) (DoctorReport, error) {
			var tsErr error
			if _, err := b.TailscaleIPv4(); err != nil {
				tsErr = err
			}
			if _, err := b.TailscaleDNS(); err != nil {
				if tsErr == nil {
					tsErr = err
				} else {
					tsErr = fmt.Errorf("%v; %w", tsErr, err)
				}
			}
			// doctor reports a down daemon; it never starts one.
			daemonErr := b.Client().Health()

			report := DoctorReport{TailscaleOK: tsErr == nil, DaemonOK: daemonErr == nil}
			if tsErr != nil {
				report.TailscaleError = tsErr.Error()
			}
			if daemonErr != nil {
				report.DaemonError = daemonErr.Error()
			}
			if tsErr != nil || daemonErr != nil {
				return report, &op.Error{Kind: op.KindError, Code: "health_check_failed", Message: "ferry doctor detected issues", Result: report}
			}
			return report, nil
		},
		Render: func(w io.Writer, r DoctorReport) error {
			if r.TailscaleOK {
				_, _ = fmt.Fprintln(w, "tailscale: ok")
			} else {
				_, _ = fmt.Fprintf(w, "tailscale: error (%s)\n", r.TailscaleError)
			}
			var err error
			if r.DaemonOK {
				_, err = fmt.Fprintln(w, "daemon: ok")
			} else {
				_, err = fmt.Fprintf(w, "daemon: error (%s)\n", r.DaemonError)
			}
			return err
		},
	})
}
