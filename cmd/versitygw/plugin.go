package main

import (
	"errors"
	"fmt"
	"plugin"
	"strings"

	"github.com/urfave/cli/v2"
	"github.com/versity/versitygw/plugins"
)

func pluginCommand() *cli.Command {
	return &cli.Command{
		Name:   "plugin",
		Usage:  "load a backend from a plugin",
		Action: runPluginBackend,
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "option",
				Usage:   "the options to pass to the backend plugin",
				Aliases: []string{"o"},
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
	optionFlag := ctx.StringSlice("option")

	opts, err := parseOptionsToMap(optionFlag)
	if err != nil {
		return err
	}

	p, err := plugin.Open(pluginPath)
	if err != nil {
		return err
	}

	backendSymbol, err := p.Lookup("Backend")
	if err != nil {
		return err
	}

	backendPlugin, ok := backendSymbol.(plugins.BackendPlugin)
	if !ok {
		return errors.New("plugin is not of type plugins.BackendPlugin")
	}

	be, err := backendPlugin.New(opts)
	if err != nil {
		return err
	}

	return runGateway(ctx.Context, be)
}
