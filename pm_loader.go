package pm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/telnet2/pm/envmap"
	"gopkg.in/yaml.v3"
)

type ConfigFile struct {
	Services map[string]*Runnable `yaml:"services"`
}

// WriteFile writes *ConfigFile into a file
func (cf *ConfigFile) WriteFile(yamlFile string) error {
	data, err := yaml.Marshal(cf)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlFile, data, 0640)
}

func (cf *ConfigFile) String() string {
	data, _ := yaml.Marshal(cf)
	return string(data)
}

// ExpandEnvs expands env vars used in this runnable.
func ExpandEnvs(r *Runnable) {
	envs := make(envmap.EnvMap)

	// 1. Load system env vars.
	envs.LoadSystem()

	// 2. Load the env list.
	envs.Load(r.Env)

	// 3. Load from env files
	envs.LoadFile(calculateRelDir(r.ConfDir, r.EnvFile))

	// 4. Expand env-vars that appear in the fields with the "envexp" tag.
	envs.Expand(r)

	// Expand variables
	if r.CommandEx != "" {
		// If CommandEx exists, use it without expansion.
		r.Command = r.CommandEx
		r.CommandEx = ""
	}

	r.envMap = envs
}

// LoadYaml loads config from io.Reader
func LoadYaml(ctx context.Context, r io.Reader) (*ConfigFile, error) {
	data := ConfigFile{}
	decoder := yaml.NewDecoder(r)
	for {
		if err := decoder.Decode(&data); err != nil {
			if err == io.EOF {
				for id, s := range data.Services {
					// Replace a new line
					s.Command = regexp.MustCompile(`[\r\n\t]`).ReplaceAllString(s.Command, "")
					s.Id = id
					s.ConfDir = CtxConfDir(ctx)
					ExpandEnvs(s)
					s.LogDir = calculateRelDir(s.ConfDir, s.LogDir)
					s.WorkDir = calculateRelDir(s.ConfDir, s.WorkDir)

					if s.LogDir != "" {
						_ = os.MkdirAll(s.LogDir, os.ModePerm)
						if fi, err := os.Stat(s.LogDir); err != nil || !fi.IsDir() {
							return nil, fmt.Errorf("can't open log_dir: %s", s.LogDir)
						}
					}
				}
				return &data, nil
			} else {
				return nil, err
			}
		}
	}
}

// calculateRelDir calculates the abs dir from a given relative with respect to the config file's dir.
func calculateRelDir(base, dir string) string {
	if dir != "" {
		// if `dir`` is relative dir,
		// then calculate its directory based on the config file's dir.
		if !filepath.IsAbs(dir) {
			return filepath.Join(base, dir)
		}
	}
	return dir
}

// LoadYamlFile loads a config from a given yaml file.
func LoadYamlFile(ctx context.Context, f string) (*ConfigFile, error) {
	if filepath.IsAbs(f) {
		// if the file is in absolute path, update the conf dir.
		confDir := filepath.Dir(f)
		ctx = WithConfDir(ctx, confDir)
	} else {
		// if the file is relative path, combine with the conf dir.
		confDir := CtxConfDir(ctx)
		if confDir != "" {
			f = filepath.Join(confDir, f)
		} else {
			ctx = WithConfDir(ctx, filepath.Dir(f))
		}
	}

	fl, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	return LoadYaml(ctx, fl)
}
