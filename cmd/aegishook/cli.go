package main

import (
	"aegishook/internal/core"
	"aegishook/internal/localaddr"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type options struct {
	command, dataDir, addr, agent, scope, project, id, agentDir string
	publicURL, clientBinaries, trustedProxy                     string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	o := options{command: "serve"}
	if len(args) > 0 && args[0] == "help" {
		args = append(append([]string{}, args[1:]...), "-h")
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.command, args = args[0], args[1:]
	}
	switch o.command {
	case "serve", "install", "uninstall", "status":
	default:
		return o, fmt.Errorf("unknown command %q; run aegishook -h for help", o.command)
	}
	f := flag.NewFlagSet("aegishook "+o.command, flag.ContinueOnError)
	f.SetOutput(output)
	home, _ := os.UserHomeDir()
	f.StringVar(&o.dataDir, "data-dir", filepath.Join(home, ".aegishook"), "private data directory")
	host := f.String("host", "127.0.0.1", "loopback address: localhost, 127.x.x.x or ::1")
	port := f.Int("port", 18790, "listen port (1-65535)")
	f.StringVar(&o.addr, "addr", "", "full address host:port (IPv6 as [::1]:port); cannot be combined with --host/--port")
	if o.command == "serve" {
		f.StringVar(&o.clientBinaries, "client-binaries", "", "self-hosted client binaries directory; downloaded from the Release if omitted")
		f.StringVar(&o.trustedProxy, "trusted-proxies", "", "proxy CIDRs allowed to set X-Real-IP, comma-separated; none trusted by default")
		f.StringVar(&o.publicURL, "public-url", "", "public origin of the HTTPS reverse proxy; local access only by default")
	}
	if o.command == "serve" || o.command == "install" {
		f.StringVar(&o.agentDir, "agent-dir", core.AgentDir(), "Pi user directory")
	} else {
		o.agentDir = core.AgentDir()
	}
	if o.command == "install" {
		f.StringVar(&o.agent, "agent", "pi", "client: pi, claude, codex, opencode, grok")
		f.StringVar(&o.scope, "scope", "global", "install scope: global or project")
		f.StringVar(&o.project, "project", "", "absolute project path (used with --scope project)")
	}
	if o.command == "uninstall" {
		f.StringVar(&o.id, "id", "", "install record ID to uninstall (see status)")
	}
	f.Usage = func() {
		fmt.Fprintf(output, "AegisHook %s · local agent execution review console\n\nUsage:\n  aegishook [command] [options]\n\nCommands:\n  serve       start the console (default)\n  install     install an agent hook\n  uninstall   uninstall a specific hook\n  status      show agents, install records and sessions\n  hook        handle client hook events\n  version     show the version (also --version)\n\n%s options:\n", core.ReleaseVersion, o.command)
		f.PrintDefaults()
		fmt.Fprint(output, "  -h, --help\n        show help; run aegishook <command> -h for that command's options\n\nExamples:\n  aegishook --host localhost --port 18800\n  aegishook serve --host ::1 --port 18800\n  aegishook serve --addr 127.0.0.1:18800\n  aegishook install --agent claude --host localhost --port 18800\n  aegishook status --host localhost --port 18800\n\nOnly loopback addresses are allowed. When you use a custom address or port, every command must use the same connection options and data directory.\n")
	}
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 {
		return o, fmt.Errorf("unsupported positional arguments %q; run aegishook %s -h for help", f.Args(), o.command)
	}
	seen := map[string]bool{}
	f.Visit(func(v *flag.Flag) { seen[v.Name] = true })
	if seen["addr"] {
		if seen["host"] || seen["port"] {
			return o, fmt.Errorf("--addr cannot be combined with --host or --port")
		}
		var portText string
		var err error
		*host, portText, err = net.SplitHostPort(o.addr)
		if err != nil {
			return o, fmt.Errorf("--addr must be host:port (use [::1]:port for IPv6): %w", err)
		}
		*port, err = strconv.Atoi(portText)
		if err != nil {
			return o, fmt.Errorf("port must be an integer from 1 to 65535")
		}
	}
	if !localaddr.IsLoopback(*host) {
		return o, fmt.Errorf("only loopback addresses are supported (localhost, 127.x.x.x or ::1), got %q", *host)
	}
	if *port < 1 || *port > 65535 {
		return o, fmt.Errorf("port must be between 1 and 65535, got %d", *port)
	}
	o.addr = net.JoinHostPort(strings.ToLower(*host), strconv.Itoa(*port))
	if o.trustedProxy != "" {
		for _, v := range strings.Split(o.trustedProxy, ",") {
			if _, err := netip.ParsePrefix(strings.TrimSpace(v)); err != nil {
				return o, fmt.Errorf("--trusted-proxies must be comma-separated IP CIDRs")
			}
		}
	}
	if o.publicURL != "" {
		var err error
		o.publicURL, err = localaddr.Endpoint(o.publicURL)
		if err != nil || !strings.HasPrefix(o.publicURL, "https://") {
			return o, fmt.Errorf("--public-url must be an HTTPS origin")
		}
	}
	return o, nil
}
