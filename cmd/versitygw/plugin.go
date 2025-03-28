package main

import (
	"errors"
	"fmt"
	"os"
	"plugin"
	"strings"

	"github.com/urfave/cli/v2"
	"github.com/versity/versitygw/plugins"
	"sigs.k8s.io/yaml"
)

func pluginCommand() *cli.Command {
	return &cli.Command{
		Name:   "plugin",
		Usage:  "load a backend from a plugin",
		Action: runPluginBackend,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "location of the config file",
				Aliases: []string{"c"},
			},
		},
	}
}

func parseOptionsToMap(opts []string) (map[string]string, error) {
	m := make(map[string]string, len(opts))
	for _, o := range opts {
		parts := strings.SplitN(o, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("option %s not in expected format <key>=<value>", o)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}

func runPluginBackend(ctx *cli.Context) error {
	if ctx.NArg() == 0 {
		return fmt.Errorf("no plugin file provided to be loaded")
	}

	pluginPath := ctx.Args().Get(0)
	configPath := ctx.String("config")

	var config map[string]any
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config = make(map[string]any)
	} else {
		d, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(d, &config); err != nil {
			return err
		}
	}

	p, err := plugin.Open(pluginPath)
	if err != nil {
		return err
	}

	backendSymbol, err := p.Lookup("Backend")
	if err != nil {
		return err
	}
	backendPluginPtr, ok := backendSymbol.(*plugins.BackendPlugin)
	if !ok {
		return errors.New("plugin is not of type *plugins.BackendPlugin")
	}

	if backendPluginPtr == nil {
		return errors.New("Backend is nil")
	}

	be, err := (*backendPluginPtr).New(config)
	if err != nil {
		return err
	}

	return runGateway(ctx.Context, be)
}
