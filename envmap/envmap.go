package envmap

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"

	shellexpand "github.com/ganbarodigital/go_shellexpand"
	"github.com/jinzhu/copier"
	"github.com/joho/godotenv"
	"github.com/mitchellh/reflectwalk"
	"github.com/telnet2/pm/debug"
)

var (
	_log = debug.NewDebugLogger("pm:envmap")
)

type EnvMap map[string]string

// AsString returns a list of KEY=VALUE strings.
func (em EnvMap) AsString() (ret []string) {
	for k, v := range em {
		ret = append(ret, fmt.Sprintf("%s=%s", k, v))
	}
	return
}

// LoadSystem loads os.Environ() into this map
func (em EnvMap) LoadSystem() {
	em.Load(os.Environ())
}

// Load loads envs from a string array
func (em EnvMap) Load(envs []string) {
	for _, env := range envs {
		kv := strings.SplitN(env, "=", 2)
		if len(kv) == 2 {
			em[kv[0]] = kv[1]
		} else {
			_log.Printf("invalid env: %s", env)
		}
	}
}

// LoadFile load env files
func (em EnvMap) LoadFile(f ...string) {
	other, _ := godotenv.Read(f...)
	em.Merge(other)
}

// Merge merges the other env map into this env map.
func (em EnvMap) Merge(other EnvMap) {
	_ = copier.Copy(&em, other)
}

// Expand performs shell expansion using this EnvMap.
func (em EnvMap) NewExpander() func(string) string {
	callbacks := shellexpand.ExpansionCallbacks{
		LookupVar: func(s string) (string, bool) {
			v, ok := em[s]
			return v, ok
		},
	}
	return func(input string) string {
		out, _ := shellexpand.Expand(input, callbacks)
		return out
	}
}

// Expand expands struct fields with the "envexp" tag.
func (em EnvMap) Expand(t interface{}) {
	expander := varExpander{expander: em.NewExpander(), environ: em.AsString()}
	_ = expander.Expand(t)
}

// varExpander is a helper type to recursively expand struct fields with `envexp` tag.
type varExpander struct {
	environ  []string
	expander func(string) string
}

func (em *varExpander) Expand(t interface{}) error {
	return reflectwalk.Walk(t, em)
}

func (em *varExpander) Struct(v reflect.Value) error {
	return nil
}

func (em *varExpander) StructField(f reflect.StructField, v reflect.Value) error {
	if f.Type.Kind() == reflect.String {
		_, hasTag := f.Tag.Lookup("envexp")
		if hasTag && v.CanSet() {
			org := v.String()
			v.SetString(em.expander(org))
		}
		return nil
	}
	return nil
}

// ExpandShellVars returns a string after shell variables are expanded by `bash -c "echo \"$(cmd)\""` command.
// If any error happens, the original `cmd` is returned.
func ExpandShellVars(cmd string, envs []string) string {
	proc := exec.Command("bash", "-c", fmt.Sprintf("echo -n \"%s\"", cmd))
	proc.Env = envs
	out, err := proc.CombinedOutput()
	if err != nil {
		return cmd
	}
	cmd = strings.ReplaceAll(cmd, "\n", "\\n")
	_log.Warnf("fail to expand command: `%s`", cmd)
	return string(out)
}
