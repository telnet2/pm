package pm

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadYaml(t *testing.T) {
	ctx := context.Background()
	ctx = WithConfDir(ctx, "./testdata")

	configFile, err := LoadYamlFile(ctx, "config_test.yaml")
	assert.NoError(t, err)
	assert.Len(t, configFile.Services, 4)

	ctx = WithConfDir(ctx, "")
	configFile, err = LoadYamlFile(ctx, "./testdata/config_test.yaml")
	assert.NoError(t, err)
	assert.Equal(t, "testdata/logs", configFile.Services["task1"].LogDir)
	assert.Equal(t, ".", configFile.Services["task1"].WorkDir)

	assert.Len(t, configFile.Services, 4)
	assert.Contains(t, configFile.Services, "task1")
	assert.Contains(t, configFile.Services, "task2")
	assert.Contains(t, configFile.Services["task2"].DependsOn, "task1")

	assert.Equal(t, configFile.Services["task1"].Id, "task1")
	assert.Equal(t, configFile.Services["task2"].Id, "task2")
	assert.Equal(t, configFile.Services["task2"].Command, "echo \"ls done\"")

	assert.Equal(t, configFile.Services["task4"].WorkDir, os.Getenv("HOME"))

	t3 := configFile.Services["task3"]
	// Override A using config_test.env file
	assert.Equal(t, "env -conf=ENVFILE -log=D", t3.Command)
}
