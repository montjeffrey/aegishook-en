package main

import (
	"aegishook/internal/assets"
	"aegishook/internal/core"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runClient(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall") {
		return errors.New("usage: aegishook client install|uninstall --agent AGENT --scope global|project [options]")
	}
	action := args[0]
	f := flag.NewFlagSet("client "+action, flag.ContinueOnError)
	f.SetOutput(output)
	home, _ := os.UserHomeDir()
	o := core.ClientOptions{Progress: os.Stderr}
	f.StringVar(&o.Dir, "client-dir", filepath.Join(home, ".aegishook-client"), "private hook client directory (not the server data directory)")
	f.StringVar(&o.Agent, "agent", "claude", "pi, claude, codex, opencode or grok")
	f.StringVar(&o.Scope, "scope", "global", "global or project")
	f.StringVar(&o.Project, "project", "", "absolute project path")
	f.StringVar(&o.PiDir, "agent-dir", core.AgentDir(), "Pi user directory")
	codeFile := ""
	codeStdin := false
	if action == "install" {
		f.StringVar(&o.Endpoint, "endpoint", "", "review service origin: HTTPS, or HTTP on loopback")
		f.StringVar(&codeFile, "code-file", "", "one-time device enrollment code file")
		f.BoolVar(&codeStdin, "code-stdin", false, "read the one-time enrollment code from stdin")
		f.BoolVar(&o.Switch, "switch", false, "on successful install, remove the same agent's other scope (global/project)")
	}
	if e := f.Parse(args[1:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unsupported positional arguments")
	}
	if action == "install" {
		if codeFile != "" && codeStdin {
			return errors.New("--code-file and --code-stdin cannot be used together")
		}
		source := input
		if codeFile != "" {
			file, e := os.Open(codeFile)
			if e != nil {
				return errors.New("cannot read the one-time enrollment code file")
			}
			defer file.Close()
			source = file
		}
		var b []byte
		var e error
		if codeFile != "" || codeStdin {
			b, e = io.ReadAll(io.LimitReader(source, 8193))
		}
		if e != nil || len(b) > 8192 {
			return errors.New("cannot read the one-time enrollment code, or it is too long")
		}
		o.EnrollmentCode = strings.TrimSpace(string(b))
		o.Binary, e = os.Executable()
		if e != nil {
			return e
		}
		o.Resources = map[string][]byte{}
		for name, resource := range map[string]string{"index.ts": "adapter.ts", "opencode.mjs": "opencode.mjs"} {
			b, e := assets.Files.ReadFile(resource)
			if e != nil {
				return e
			}
			o.Resources[name] = b
		}
	}
	result, e := core.ClientOperation(action, o)
	if e != nil {
		return e
	}
	if e = json.NewEncoder(output).Encode(result); e != nil {
		return e
	}
	fmt.Fprintln(output, "Reload or restart the agent; installation does not change the client's hook trust settings.")
	return nil
}
