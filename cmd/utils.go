package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gatewayd-io/gatewayd/config"
	gerr "github.com/gatewayd-io/gatewayd/errors"
	"github.com/google/go-github/v53/github"
	jsonSchemaGenerator "github.com/invopop/jsonschema"
	"github.com/knadh/koanf"
	koanfJson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	jsonSchemaV5 "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/cobra"
)

type (
	configFileType string
)

const (
	FilePermissions     os.FileMode = 0o644
	ExecFilePermissions os.FileMode = 0o755
	ExecFileMask        os.FileMode = 0o111
	MaxFileSize         int64       = 1024 * 1024 * 100 // 10MB
)

var (
	Global  configFileType = "global"
	Plugins configFileType = "plugins"

	DSN = "https://e22f42dbb3e0433fbd9ea32453faa598@o4504550475038720.ingest.sentry.io/4504550481723392"
)

// generateConfig generates a config file of the given type.
func generateConfig(
	cmd *cobra.Command, fileType configFileType, configFile string, forceRewriteFile bool,
) {
	logger := log.New(cmd.OutOrStdout(), "", 0)

	// Create a new config object and load the defaults.
	conf := &config.Config{
		GlobalKoanf: koanf.New("."),
		PluginKoanf: koanf.New("."),
	}
	conf.LoadDefaults(context.TODO())

	// Marshal the config file to YAML.
	var konfig *koanf.Koanf
	switch fileType {
	case Global:
		konfig = conf.GlobalKoanf
	case Plugins:
		konfig = conf.PluginKoanf
	default:
		logger.Fatal("Invalid config file type")
	}
	cfg, err := konfig.Marshal(yaml.Parser())
	if err != nil {
		logger.Fatal(err)
	}

	// Check if the config file already exists and if we should overwrite it.
	exists := false
	if _, err := os.Stat(configFile); err == nil && !forceRewriteFile {
		logger.Fatal(
			"Config file already exists. Use --force to overwrite or choose a different filename.")
	} else if err == nil {
		exists = true
	}

	// Create or overwrite the config file.
	if err := os.WriteFile(configFile, cfg, FilePermissions); err != nil {
		logger.Fatal(err)
	}

	verb := "created"
	if exists && forceRewriteFile {
		verb = "overwritten"
	}
	cmd.Printf("Config file '%s' was %s successfully.", configFile, verb)
}

func lintConfig(fileType configFileType, configFile string) error {
	// Load the config file and check it for errors.
	var conf *config.Config
	switch fileType {
	case Global:
		conf = config.NewConfig(context.TODO(), configFile, "")
		conf.LoadDefaults(context.TODO())
		conf.LoadGlobalConfigFile(context.TODO())
		conf.UnmarshalGlobalConfig(context.TODO())
	case Plugins:
		conf = config.NewConfig(context.TODO(), "", configFile)
		conf.LoadDefaults(context.TODO())
		conf.LoadPluginConfigFile(context.TODO())
		conf.UnmarshalPluginConfig(context.TODO())
	default:
		return gerr.ErrLintingFailed
	}

	// Marshal the config to JSON.
	var jsonData []byte
	var err error
	switch fileType {
	case Global:
		jsonData, err = conf.GlobalKoanf.Marshal(koanfJson.Parser())
	case Plugins:
		jsonData, err = conf.PluginKoanf.Marshal(koanfJson.Parser())
	default:
		return gerr.ErrLintingFailed
	}
	if err != nil {
		return gerr.ErrLintingFailed.Wrap(err)
	}

	// Unmarshal the JSON data into a map.
	var jsonBytes map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonBytes)
	if err != nil {
		return gerr.ErrLintingFailed.Wrap(err)
	}

	// Generate a JSON schema from the config struct.
	var generatedSchema *jsonSchemaGenerator.Schema
	switch fileType {
	case Global:
		generatedSchema = jsonSchemaGenerator.Reflect(&config.GlobalConfig{})
	case Plugins:
		generatedSchema = jsonSchemaGenerator.Reflect(&config.PluginConfig{})
	default:
		return gerr.ErrLintingFailed
	}

	// Marshal the schema to JSON.
	schemaBytes, err := json.Marshal(generatedSchema)
	if err != nil {
		return gerr.ErrLintingFailed.Wrap(err)
	}

	// Compile the schema for validation.
	schema, err := jsonSchemaV5.CompileString("", string(schemaBytes))
	if err != nil {
		return gerr.ErrLintingFailed.Wrap(err)
	}

	// Validate the config against the schema.
	err = schema.Validate(jsonBytes)
	if err != nil {
		return gerr.ErrLintingFailed.Wrap(err)
	}

	return nil
}

func listPlugins(cmd *cobra.Command, pluginConfigFile string, onlyEnabled bool) {
	// Load the plugin config file.
	conf := config.NewConfig(context.TODO(), "", pluginConfigFile)
	conf.LoadDefaults(context.TODO())
	conf.LoadPluginConfigFile(context.TODO())
	conf.UnmarshalPluginConfig(context.TODO())

	if len(conf.Plugin.Plugins) != 0 {
		cmd.Printf("Total plugins: %d\n", len(conf.Plugin.Plugins))
		cmd.Println("Plugins:")
	} else {
		cmd.Println("No plugins found")
	}

	// Print the list of plugins.
	for _, plugin := range conf.Plugin.Plugins {
		if onlyEnabled && !plugin.Enabled {
			continue
		}
		cmd.Printf("  Name: %s\n", plugin.Name)
		cmd.Printf("  Enabled: %t\n", plugin.Enabled)
		cmd.Printf("  Path: %s\n", plugin.LocalPath)
		cmd.Printf("  Args: %s\n", strings.Join(plugin.Args, " "))
		cmd.Println("  Env:")
		for _, env := range plugin.Env {
			cmd.Printf("    %s\n", env)
		}
		cmd.Printf("  Checksum: %s\n", plugin.Checksum)
	}
}

func extractZip(filename, dest string) ([]string, error) {
	// Open and extract the zip file.
	zipRc, err := zip.OpenReader(filename)
	if err != nil {
		return nil, gerr.ErrExtractFailed.Wrap(err)
	}
	defer zipRc.Close()

	// Create the output directory if it doesn't exist.
	if err := os.MkdirAll(dest, FolderPermissions); err != nil {
		return nil, gerr.ErrExtractFailed.Wrap(err)
	}

	// Extract the files.
	filenames := []string{}
	for _, file := range zipRc.File {
		switch fileInfo := file.FileInfo(); {
		case fileInfo.IsDir():
			// Sanitize the path.
			filename := filepath.Clean(file.Name)
			if !path.IsAbs(filename) {
				destPath := path.Join(dest, filename)
				// Create the directory.

				if err := os.MkdirAll(destPath, FolderPermissions); err != nil {
					return nil, gerr.ErrExtractFailed.Wrap(err)
				}
			}
		case fileInfo.Mode().IsRegular():
			// Sanitize the path.
			outFilename := filepath.Join(filepath.Clean(dest), filepath.Clean(file.Name))

			// Check for ZipSlip.
			if strings.HasPrefix(outFilename, string(os.PathSeparator)) {
				return nil, gerr.ErrExtractFailed.Wrap(
					fmt.Errorf("illegal file path: %s", outFilename))
			}

			// Create the file.
			outFile, err := os.Create(outFilename)
			if err != nil {
				return nil, gerr.ErrExtractFailed.Wrap(err)
			}
			defer outFile.Close()

			// Open the file in the zip archive.
			fileRc, err := file.Open()
			if err != nil {
				os.Remove(outFilename)
				return nil, gerr.ErrExtractFailed.Wrap(err)
			}

			// Copy the file contents.
			if _, err := io.Copy(outFile, io.LimitReader(fileRc, MaxFileSize)); err != nil {
				os.Remove(outFilename)
				return nil, gerr.ErrExtractFailed.Wrap(err)
			}

			fileMode := file.FileInfo().Mode()
			// Set the file permissions.
			if fileMode.IsRegular() && fileMode&ExecFileMask != 0 {
				if err := os.Chmod(outFilename, ExecFilePermissions); err != nil {
					return nil, gerr.ErrExtractFailed.Wrap(err)
				}
			} else {
				if err := os.Chmod(outFilename, FilePermissions); err != nil {
					return nil, gerr.ErrExtractFailed.Wrap(err)
				}
			}

			filenames = append(filenames, outFile.Name())
		default:
			return nil, gerr.ErrExtractFailed.Wrap(
				fmt.Errorf("unknown file type: %s", file.Name))
		}
	}

	return filenames, nil
}

func extractTarGz(filename, dest string) ([]string, error) {
	// Open and extract the tar.gz file.
	gzipStream, err := os.Open(filename)
	if err != nil {
		return nil, gerr.ErrExtractFailed.Wrap(err)
	}
	defer gzipStream.Close()

	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return nil, gerr.ErrExtractFailed.Wrap(err)
	}

	// Create the output directory if it doesn't exist.
	if err := os.MkdirAll(dest, FolderPermissions); err != nil {
		return nil, gerr.ErrExtractFailed.Wrap(err)
	}

	tarReader := tar.NewReader(uncompressedStream)
	filenames := []string{}

	for {
		header, err := tarReader.Next()

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, gerr.ErrExtractFailed.Wrap(err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Sanitize the path
			cleanPath := filepath.Clean(header.Name)
			// Ensure it is not an absolute path
			if !path.IsAbs(cleanPath) {
				destPath := path.Join(dest, cleanPath)
				if err := os.MkdirAll(destPath, FolderPermissions); err != nil {
					return nil, gerr.ErrExtractFailed.Wrap(err)
				}
			}
		case tar.TypeReg:
			// Sanitize the path
			outFilename := path.Join(filepath.Clean(dest), filepath.Clean(header.Name))

			// Check for TarSlip.
			if strings.HasPrefix(outFilename, string(os.PathSeparator)) {
				return nil, gerr.ErrExtractFailed.Wrap(err)
			}

			// Create the file.
			outFile, err := os.Create(outFilename)
			if err != nil {
				return nil, gerr.ErrExtractFailed.Wrap(err)
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, io.LimitReader(tarReader, MaxFileSize)); err != nil {
				os.Remove(outFilename)
				return nil, gerr.ErrExtractFailed.Wrap(err)
			}

			fileMode := header.FileInfo().Mode()
			// Set the file permissions
			if fileMode.IsRegular() && fileMode&ExecFileMask != 0 {
				if err := os.Chmod(outFilename, ExecFilePermissions); err != nil {
					return nil, gerr.ErrExtractFailed.Wrap(err)
				}
			} else {
				if err := os.Chmod(outFilename, FilePermissions); err != nil {
					return nil, gerr.ErrExtractFailed.Wrap(err)
				}
			}

			filenames = append(filenames, outFile.Name())
		default:
			return nil, gerr.ErrExtractFailed.Wrap(
				fmt.Errorf("unknown file type: %s", header.Name))
		}
	}

	return filenames, nil
}

func findAsset(release *github.RepositoryRelease, match func(string) bool) (string, string, int64) {
	if release == nil {
		return "", "", 0
	}

	// Find the matching release.
	for _, asset := range release.Assets {
		if match(asset.GetName()) {
			return asset.GetName(), asset.GetBrowserDownloadURL(), asset.GetID()
		}
	}
	return "", "", 0
}

func downloadFile(
	client *github.Client, account, pluginName string, releaseID int64, filename string,
) (string, error) {
	// Download the plugin.
	readCloser, redirectURL, err := client.Repositories.DownloadReleaseAsset(
		context.Background(), account, pluginName, releaseID, http.DefaultClient)
	if err != nil {
		return "", gerr.ErrDownloadFailed.Wrap(err)
	}
	defer readCloser.Close()

	if redirectURL != "" {
		// Download the plugin from the redirect URL.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirectURL, nil)
		if err != nil {
			return "", gerr.ErrDownloadFailed.Wrap(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", gerr.ErrDownloadFailed.Wrap(err)
		}
		defer resp.Body.Close()

		readCloser = resp.Body
	}

	if readCloser == nil {
		return "", gerr.ErrDownloadFailed.Wrap(
			fmt.Errorf("unable to download file: %s", filename))
	}

	// Create the output file in the current directory and write the downloaded content.
	cwd, err := os.Getwd()
	if err != nil {
		return "", gerr.ErrDownloadFailed.Wrap(err)
	}
	filePath := path.Join([]string{cwd, filename}...)
	output, err := os.Create(filePath)
	if err != nil {
		return "", gerr.ErrDownloadFailed.Wrap(err)
	}
	defer output.Close()

	// Write the bytes to the file.
	_, err = io.Copy(output, readCloser)
	if err != nil {
		return "", gerr.ErrDownloadFailed.Wrap(err)
	}

	return filePath, nil
}

// deleteFiles deletes the files in the toBeDeleted list.
func deleteFiles(toBeDeleted []string) {
	for _, filename := range toBeDeleted {
		if err := os.Remove(filename); err != nil {
			log.Println("There was an error deleting the file: ", err)
			return
		}
	}
}
