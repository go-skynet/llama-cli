package gallery

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-skynet/LocalAI/pkg/utils"
	"github.com/imdario/mergo"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
)

/*

description: |
    foo
license: ""

urls:
-
-

name: "bar"

config_file: |
    # Note, name will be injected. or generated by the alias wanted by the user
    threads: 14

files:
    - filename: ""
      sha: ""
      uri: ""

prompt_templates:
    - name: ""
      content: ""

*/
// Config is the model configuration which contains all the model details
// This configuration is read from the gallery endpoint and is used to download and install the model
type Config struct {
	Description     string           `yaml:"description"`
	License         string           `yaml:"license"`
	URLs            []string         `yaml:"urls"`
	Name            string           `yaml:"name"`
	ConfigFile      string           `yaml:"config_file"`
	Files           []File           `yaml:"files"`
	PromptTemplates []PromptTemplate `yaml:"prompt_templates"`
}

type File struct {
	Filename string `yaml:"filename" json:"filename"`
	SHA256   string `yaml:"sha256" json:"sha256"`
	URI      string `yaml:"uri" json:"uri"`
}

type PromptTemplate struct {
	Name    string `yaml:"name"`
	Content string `yaml:"content"`
}

func GetGalleryConfigFromURL(url string) (Config, error) {
	var config Config
	err := utils.GetURI(url, func(url string, d []byte) error {
		return yaml.Unmarshal(d, &config)
	})
	if err != nil {
		log.Debug().Msgf("GetGalleryConfigFromURL error for url %s\n%s", url, err.Error())
		return config, err
	}
	return config, nil
}

func ReadConfigFile(filePath string) (*Config, error) {
	// Read the YAML file
	yamlFile, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file: %v", err)
	}

	// Unmarshal YAML data into a Config struct
	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %v", err)
	}

	return &config, nil
}

func InstallModel(basePath, nameOverride string, config *Config, configOverrides map[string]interface{}, downloadStatus func(string, string, string, float64)) error {
	// Create base path if it doesn't exist
	err := os.MkdirAll(basePath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create base path: %v", err)
	}

	if len(configOverrides) > 0 {
		log.Debug().Msgf("Config overrides %+v", configOverrides)
	}

	// Download files and verify their SHA
	for _, file := range config.Files {
		log.Debug().Msgf("Checking %q exists and matches SHA", file.Filename)

		if err := utils.VerifyPath(file.Filename, basePath); err != nil {
			return err
		}
		// Create file path
		filePath := filepath.Join(basePath, file.Filename)

		// Check if the file already exists
		_, err := os.Stat(filePath)
		if err == nil {
			// File exists, check SHA
			if file.SHA256 != "" {
				// Verify SHA
				calculatedSHA, err := calculateSHA(filePath)
				if err != nil {
					return fmt.Errorf("failed to calculate SHA for file %q: %v", file.Filename, err)
				}
				if calculatedSHA == file.SHA256 {
					// SHA matches, skip downloading
					log.Debug().Msgf("File %q already exists and matches the SHA. Skipping download", file.Filename)
					continue
				}
				// SHA doesn't match, delete the file and download again
				err = os.Remove(filePath)
				if err != nil {
					return fmt.Errorf("failed to remove existing file %q: %v", file.Filename, err)
				}
				log.Debug().Msgf("Removed %q (SHA doesn't match)", filePath)

			} else {
				// SHA is missing, skip downloading
				log.Debug().Msgf("File %q already exists. Skipping download", file.Filename)
				continue
			}
		} else if !os.IsNotExist(err) {
			// Error occurred while checking file existence
			return fmt.Errorf("failed to check file %q existence: %v", file.Filename, err)
		}

		log.Debug().Msgf("Downloading %q", file.URI)

		// Download file
		resp, err := http.Get(file.URI)
		if err != nil {
			return fmt.Errorf("failed to download file %q: %v", file.Filename, err)
		}
		defer resp.Body.Close()

		// Create parent directory
		err = os.MkdirAll(filepath.Dir(filePath), 0755)
		if err != nil {
			return fmt.Errorf("failed to create parent directory for file %q: %v", file.Filename, err)
		}

		// Create and write file content
		outFile, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create file %q: %v", file.Filename, err)
		}
		defer outFile.Close()

		progress := &progressWriter{
			fileName:       file.Filename,
			total:          resp.ContentLength,
			hash:           sha256.New(),
			downloadStatus: downloadStatus,
		}
		_, err = io.Copy(io.MultiWriter(outFile, progress), resp.Body)
		if err != nil {
			return fmt.Errorf("failed to write file %q: %v", file.Filename, err)
		}

		if file.SHA256 != "" {
			// Verify SHA
			calculatedSHA := fmt.Sprintf("%x", progress.hash.Sum(nil))
			if calculatedSHA != file.SHA256 {
				log.Debug().Msgf("SHA mismatch for file %q ( calculated: %s != metadata: %s )", file.Filename, calculatedSHA, file.SHA256)
				return fmt.Errorf("SHA mismatch for file %q ( calculated: %s != metadata: %s )", file.Filename, calculatedSHA, file.SHA256)
			}
		} else {
			log.Debug().Msgf("SHA missing for %q. Skipping validation", file.Filename)
		}

		log.Debug().Msgf("File %q downloaded and verified", file.Filename)
		if utils.IsArchive(filePath) {
			log.Debug().Msgf("File %q is an archive, uncompressing to %s", file.Filename, basePath)
			if err := utils.ExtractArchive(filePath, basePath); err != nil {
				log.Debug().Msgf("Failed decompressing %q: %s", file.Filename, err.Error())
				return err
			}
		}
	}

	// Write prompt template contents to separate files
	for _, template := range config.PromptTemplates {
		if err := utils.VerifyPath(template.Name+".tmpl", basePath); err != nil {
			return err
		}
		// Create file path
		filePath := filepath.Join(basePath, template.Name+".tmpl")

		// Create parent directory
		err := os.MkdirAll(filepath.Dir(filePath), 0755)
		if err != nil {
			return fmt.Errorf("failed to create parent directory for prompt template %q: %v", template.Name, err)
		}
		// Create and write file content
		err = os.WriteFile(filePath, []byte(template.Content), 0644)
		if err != nil {
			return fmt.Errorf("failed to write prompt template %q: %v", template.Name, err)
		}

		log.Debug().Msgf("Prompt template %q written", template.Name)
	}

	name := config.Name
	if nameOverride != "" {
		name = nameOverride
	}

	if err := utils.VerifyPath(name+".yaml", basePath); err != nil {
		return err
	}

	// write config file
	if len(configOverrides) != 0 || len(config.ConfigFile) != 0 {
		configFilePath := filepath.Join(basePath, name+".yaml")

		// Read and update config file as map[string]interface{}
		configMap := make(map[string]interface{})
		err = yaml.Unmarshal([]byte(config.ConfigFile), &configMap)
		if err != nil {
			return fmt.Errorf("failed to unmarshal config YAML: %v", err)
		}

		configMap["name"] = name

		if err := mergo.Merge(&configMap, configOverrides, mergo.WithOverride); err != nil {
			return err
		}

		// Write updated config file
		updatedConfigYAML, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("failed to marshal updated config YAML: %v", err)
		}

		err = os.WriteFile(configFilePath, updatedConfigYAML, 0644)
		if err != nil {
			return fmt.Errorf("failed to write updated config file: %v", err)
		}

		log.Debug().Msgf("Written config file %s", configFilePath)
	}

	return nil
}

type progressWriter struct {
	fileName       string
	total          int64
	written        int64
	downloadStatus func(string, string, string, float64)
	hash           hash.Hash
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.hash.Write(p)
	pw.written += int64(n)

	if pw.total > 0 {
		percentage := float64(pw.written) / float64(pw.total) * 100
		//log.Debug().Msgf("Downloading %s: %s/%s (%.2f%%)", pw.fileName, formatBytes(pw.written), formatBytes(pw.total), percentage)
		pw.downloadStatus(pw.fileName, formatBytes(pw.written), formatBytes(pw.total), percentage)
	} else {
		pw.downloadStatus(pw.fileName, formatBytes(pw.written), "", 0)
	}

	return
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func calculateSHA(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
