package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadSkills reads all markdown files in the specified skills directory
// and returns a formatted string containing all their contents.
func LoadSkills(skillsDir string) string {
	if skillsDir == "" {
		return ""
	}

	// Handle ~ in path
	if strings.HasPrefix(skillsDir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			skillsDir = filepath.Join(home, skillsDir[2:])
		}
	}

	files, err := os.ReadDir(skillsDir)
	if err != nil {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n\n=== AVAILABLE SKILLS ===\n")
	builder.WriteString("The following skills and guidelines are available for you to use. Please refer to them when helping the user:\n\n")

	hasSkills := false
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(skillsDir, file.Name()))
		if err != nil {
			continue
		}

		hasSkills = true
		builder.WriteString(fmt.Sprintf("--- Skill: %s ---\n%s\n\n", file.Name(), string(data)))
	}

	if !hasSkills {
		return ""
	}

	return builder.String()
}
