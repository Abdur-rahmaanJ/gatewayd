package cmd

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gatewayd-io/gatewayd/config"
	"github.com/stretchr/testify/assert"
	"github.com/zenizh/go-capturer"
)

func Test_runCmd(t *testing.T) {
	// Create a test plugins config file.
	_, err := executeCommandC(rootCmd, "plugin", "init", "--force", "-p", pluginTestConfigFile)
	assert.NoError(t, err, "plugin init command should not have returned an error")
	assert.FileExists(t, pluginTestConfigFile, "plugin init command should have created a config file")

	// Create a test config file.
	_, err = executeCommandC(rootCmd, "config", "init", "--force", "-c", globalTestConfigFile)
	assert.NoError(t, err, "configInitCmd should not return an error")
	// Check that the config file was created.
	assert.FileExists(t, globalTestConfigFile, "configInitCmd should create a config file")

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func(waitGroup *sync.WaitGroup) {
		time.Sleep(100 * time.Millisecond)

		StopGracefully(
			runCmd.Context(),
			runCmd.Context(),
			nil,
			nil,
			nil,
			nil,
			loggers[config.Default],
			servers,
			stopChan,
		)

		waitGroup.Done()
	}(&waitGroup)

	waitGroup.Add(1)
	go func(waitGroup *sync.WaitGroup) {
		// Test run command.
		output := capturer.CaptureOutput(func() {
			_, err := executeCommandC(rootCmd, "run", "-c", globalTestConfigFile, "-p", pluginTestConfigFile)
			assert.NoError(t, err, "run command should not have returned an error")
		})
		// Print the output for debugging purposes.
		runCmd.Print(output)
		// Check if GatewayD started and stopped correctly.
		assert.Contains(t,
			output,
			"GatewayD is running",
			"run command should have returned the correct output")
		assert.Contains(t,
			output,
			"Stopped all servers\n",
			"run command should have returned the correct output")

		waitGroup.Done()
	}(&waitGroup)

	waitGroup.Wait()

	// Clean up.
	assert.NoError(t, os.Remove(pluginTestConfigFile))
	assert.NoError(t, os.Remove(globalTestConfigFile))
}

func Test_runCmdWithCachePlugin(t *testing.T) {
	// TODO: Remove this once these global variables are removed from cmd/run.go.
	// https://github.com/gatewayd-io/gatewayd/issues/324
	stopChan = make(chan struct{})

	// Create a test plugins config file.
	_, err := executeCommandC(rootCmd, "plugin", "init", "--force", "-p", pluginTestConfigFile)
	assert.NoError(t, err, "plugin init command should not have returned an error")
	assert.FileExists(t, pluginTestConfigFile, "plugin init command should have created a config file")

	// Create a test config file.
	_, err = executeCommandC(rootCmd, "config", "init", "--force", "-c", globalTestConfigFile)
	assert.NoError(t, err, "configInitCmd should not return an error")
	// Check that the config file was created.
	assert.FileExists(t, globalTestConfigFile, "configInitCmd should create a config file")

	// Test plugin install command.
	output, err := executeCommandC(
		rootCmd, "plugin", "install",
		"github.com/gatewayd-io/gatewayd-plugin-cache@v0.2.4", "-p", pluginTestConfigFile)
	assert.NoError(t, err, "plugin install should not return an error")
	assert.Contains(t, output, "Downloading https://github.com/gatewayd-io/gatewayd-plugin-cache/releases/download/v0.2.4/gatewayd-plugin-cache-linux-amd64-v0.2.4.tar.gz") //nolint:lll
	assert.Contains(t, output, "Downloading https://github.com/gatewayd-io/gatewayd-plugin-cache/releases/download/v0.2.4/checksums.txt")                                   //nolint:lll
	assert.Contains(t, output, "Download completed successfully")
	assert.Contains(t, output, "Checksum verification passed")
	assert.Contains(t, output, "Plugin binary extracted to plugins/gatewayd-plugin-cache")
	assert.Contains(t, output, "Plugin installed successfully")

	// See if the plugin was actually installed.
	output, err = executeCommandC(rootCmd, "plugin", "list", "-p", pluginTestConfigFile)
	assert.NoError(t, err, "plugin list should not return an error")
	assert.Contains(t, output, "Name: gatewayd-plugin-cache")

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func(waitGroup *sync.WaitGroup) {
		time.Sleep(500 * time.Millisecond)

		StopGracefully(
			runCmd.Context(),
			runCmd.Context(),
			nil,
			nil,
			nil,
			nil,
			loggers[config.Default],
			servers,
			stopChan,
		)

		waitGroup.Done()
	}(&waitGroup)

	waitGroup.Add(1)
	go func(waitGroup *sync.WaitGroup) {
		// Test run command.
		output := capturer.CaptureOutput(func() {
			_, err := executeCommandC(rootCmd, "run", "-c", globalTestConfigFile, "-p", pluginTestConfigFile)
			assert.NoError(t, err, "run command should not have returned an error")
		})
		// Print the output for debugging purposes.
		runCmd.Print(output)
		// Check if GatewayD started and stopped correctly.
		assert.Contains(t,
			output,
			"GatewayD is running",
			"run command should have returned the correct output")
		assert.Contains(t,
			output,
			"Stopped all servers\n",
			"run command should have returned the correct output")

		waitGroup.Done()
	}(&waitGroup)

	waitGroup.Wait()

	// Clean up.
	assert.NoError(t, os.RemoveAll("plugins/"))
	assert.NoError(t, os.Remove("checksums.txt"))
	assert.NoError(t, os.Remove("gatewayd-plugin-cache-linux-amd64-v0.2.4.tar.gz"))
	assert.NoError(t, os.Remove(pluginTestConfigFile))
	assert.NoError(t, os.Remove(globalTestConfigFile))
}