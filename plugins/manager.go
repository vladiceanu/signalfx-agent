package plugins

import (
	"fmt"
	"log"
	"strings"

	"github.com/signalfx/neo-agent/config"
	"github.com/spf13/viper"
)

func configurePlugin(pluginName string, c *viper.Viper) {
	// This allows a configuration variable foo.bar to be overridable by
	// SFX_FOO_BAR=value.
	c.AutomaticEnv()
	c.SetEnvKeyReplacer(config.EnvReplacer)
	c.SetEnvPrefix(strings.ToUpper(
		fmt.Sprintf("%s_plugins_%s", config.EnvPrefix, pluginName)))
}

// Load an agent using configuration file
func Load(currentPlugins []IPlugin) ([]IPlugin, error) {
	// Load plugins.
	pluginsConfig := viper.GetStringMap("plugins")

	currentPluginSet := map[string]IPlugin{}
	for _, plugin := range currentPlugins {
		currentPluginSet[plugin.Name()] = plugin
	}

	var newPlugins []IPlugin
	var removedPlugins []IPlugin
	var reloadPlugins []IPlugin

	for pluginName := range pluginsConfig {
		pluginType := viper.GetString(fmt.Sprintf("plugins.%s.plugin", pluginName))

		if len(pluginType) < 1 {
			return nil, fmt.Errorf("plugin %s missing plugin key", pluginName)
		}

		if plugin := currentPluginSet[pluginName]; plugin != nil {
			// Already exists, just reload.
			reloadPlugins = append(reloadPlugins, plugin)
		} else {
			// New plugin
			if create, ok := Plugins[pluginType]; ok {
				log.Printf("configuring plugin %s (%s)", pluginType, pluginName)

				pluginConfig := viper.Sub("plugins." + pluginName)
				configurePlugin(pluginName, pluginConfig)
				pluginInst, err := create(pluginName, pluginConfig)
				if err != nil {
					return nil, err
				}

				newPlugins = append(newPlugins, pluginInst)
			} else {
				return nil, fmt.Errorf("unknown plugin %s", pluginType)
			}
		}
	}

	// NOTE: By this point we can't return errors with an unmodified plugin list
	// as some new plugins may have been started. If there's an old plugin 'foo'
	// and we start a new 'foo' but return an old plugin set there might be two
	// foos running.

	// Find removed plugins (in loaded plugins but not the new config).
	for _, plugin := range currentPlugins {
		if pluginsConfig[plugin.Name()] == nil {
			removedPlugins = append(removedPlugins, plugin)
		}
	}

	// Stop removed plugins.
	for _, plugin := range removedPlugins {
		log.Printf("stopping plugin %s", plugin.Name())
		plugin.Stop()
	}

	// Reload existing plugins.
	for _, plugin := range reloadPlugins {
		log.Printf("reloading plugin %s", plugin.Name())
		pluginConfig := viper.Sub("plugins." + plugin.Name())

		if pluginConfig == nil {
			log.Printf("%s plugin unexpectedly missing config", plugin.Name())
			continue
		}

		pluginType := pluginConfig.GetString("plugin")

		if pluginType != plugin.Type() {
			log.Printf("%s plugin is currently type %s but changed to %s, skipping",
				plugin.Name(), plugin.Type(), pluginType)
			continue
		}

		configurePlugin(plugin.Name(), pluginConfig)
		if err := plugin.Reload(pluginConfig); err != nil {
			log.Printf("reloading %s plugin failed: %s", plugin.Name(), err)
		}
	}

	// Start new plugins.
	for _, plugin := range newPlugins {
		log.Printf("starting plugin %s", plugin.String())
		if err := plugin.Start(); err != nil {
			log.Printf("failed to start plugin %s: %s", plugin.String(), err)
		}
	}

	return append(reloadPlugins, newPlugins...), nil
}