package terraform

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	
	"github.com/kingoftowns/tf-go/internal/constants"
)

// TerraformVariable represents a parsed Terraform variable
type TerraformVariable struct {
	Name  string
	Value interface{}
	Type  string
}

// VariableCompiler handles merging multiple tfvars files
type VariableCompiler struct {
	variables map[string]TerraformVariable
}

// NewVariableCompiler creates a new variable compiler
func NewVariableCompiler() *VariableCompiler {
	return &VariableCompiler{
		variables: make(map[string]TerraformVariable),
	}
}

// CompileVariables merges multiple tfvars files in order (later files override earlier ones)
func (vc *VariableCompiler) CompileVariables(tfvarsFiles []string) (string, error) {
	fmt.Printf("[DEBUG] Compiling variables from %d files\n", len(tfvarsFiles))

	// Process each tfvars file in order
	for i, tfvarsFile := range tfvarsFiles {
		fmt.Printf("[DEBUG] Processing tfvars file [%d]: %s\n", i+1, tfvarsFile)

		if _, err := os.Stat(tfvarsFile); os.IsNotExist(err) {
			fmt.Printf("[WARNING] Tfvars file not found: %s\n", tfvarsFile)
			continue
		}

		variables, err := vc.parseTfvarsFile(tfvarsFile)
		if err != nil {
			return "", fmt.Errorf("failed to parse %s: %w", tfvarsFile, err)
		}

		// Merge variables (later files override earlier ones)
		for name, variable := range variables {
			if existingVar, exists := vc.variables[name]; exists {
				// If both are maps, merge them instead of replacing
				if existingMap, ok := existingVar.Value.(map[string]interface{}); ok {
					if newMap, ok := variable.Value.(map[string]interface{}); ok {
						// Merge maps - new values override existing ones
						mergedMap := make(map[string]interface{})
						// Start with existing values (defaults)
						for k, v := range existingMap {
							mergedMap[k] = v
						}
						// Override with new values
						for k, v := range newMap {
							mergedMap[k] = v
							fmt.Printf("[DEBUG] Merged map field '%s.%s': %v\n", name, k, v)
						}
						vc.variables[name] = TerraformVariable{
							Name:  name,
							Value: mergedMap,
							Type:  "map",
						}
						fmt.Printf("[DEBUG] Merged variable '%s' maps\n", name)
						continue
					}
				}
				fmt.Printf("[DEBUG] Overriding variable '%s': %v -> %v\n", name, existingVar.Value, variable.Value)
			} else {
				fmt.Printf("[DEBUG] Adding variable '%s': %v\n", name, variable.Value)
			}
			vc.variables[name] = variable
		}
	}

	// Generate compiled tfvars content
	return vc.generateCompiledTfvars(), nil
}

// parseTfvarsFile parses a single .tfvars file
func (vc *VariableCompiler) parseTfvarsFile(filename string) (map[string]TerraformVariable, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	variables := make(map[string]TerraformVariable)
	scanner := bufio.NewScanner(file)
	lineNumber := 0

	// Regex patterns for different variable types
	stringVarPattern := regexp.MustCompile(`^(\w+)\s*=\s*"([^"]*)"`)
	numberVarPattern := regexp.MustCompile(`^(\w+)\s*=\s*(\d+(?:\.\d+)?)`)
	boolVarPattern := regexp.MustCompile(`^(\w+)\s*=\s*(true|false)`)
	listStartPattern := regexp.MustCompile(`^(\w+)\s*=\s*\[`)
	mapStartPattern := regexp.MustCompile(`^(\w+)\s*=\s*\{`)
	// ERB template pattern (Terraspace syntax)
	erbPattern := regexp.MustCompile(`<%=\s*expansion\(['"]([^'"]+)['"]\)\s*%>`)

	var currentListVar string
	var currentMapVar string
	var listContent []string
	var mapContent []string
	var inList, inMap bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNumber++

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		// Handle list continuation
		if inList {
			if strings.Contains(line, "]") {
				// End of list
				listContent = append(listContent, line)
				listValue := vc.parseListValue(strings.Join(listContent, "\n"))
				variables[currentListVar] = TerraformVariable{
					Name:  currentListVar,
					Value: listValue,
					Type:  "list",
				}
				inList = false
				currentListVar = ""
				listContent = []string{}
			} else {
				listContent = append(listContent, line)
			}
			continue
		}

		// Handle map continuation
		if inMap {
			if strings.Contains(line, "}") {
				// End of map
				mapContent = append(mapContent, line)
				mapValue := vc.parseMapValue(strings.Join(mapContent, "\n"))
				variables[currentMapVar] = TerraformVariable{
					Name:  currentMapVar,
					Value: mapValue,
					Type:  "map",
				}
				inMap = false
				currentMapVar = ""
				mapContent = []string{}
			} else {
				mapContent = append(mapContent, line)
			}
			continue
		}

		// Try to match different variable patterns
		if matches := stringVarPattern.FindStringSubmatch(line); matches != nil {
			value := matches[2]
			// Handle ERB template expansion
			if erbMatches := erbPattern.FindStringSubmatch(value); erbMatches != nil {
				// Replace ERB expansion with the actual value
				switch erbMatches[1] {
				case ":ENV":
					value = constants.DefaultEnvironment // Default for this case, could be made configurable
				default:
					// Keep original if we don't know how to expand it
				}
			}
			variables[matches[1]] = TerraformVariable{
				Name:  matches[1],
				Value: value,
				Type:  "string",
			}
		} else if matches := numberVarPattern.FindStringSubmatch(line); matches != nil {
			if value, err := strconv.ParseFloat(matches[2], 64); err == nil {
				variables[matches[1]] = TerraformVariable{
					Name:  matches[1],
					Value: value,
					Type:  "number",
				}
			}
		} else if matches := boolVarPattern.FindStringSubmatch(line); matches != nil {
			variables[matches[1]] = TerraformVariable{
				Name:  matches[1],
				Value: matches[2] == "true",
				Type:  "bool",
			}
		} else if matches := listStartPattern.FindStringSubmatch(line); matches != nil {
			// Start of list
			if strings.Contains(line, "]") {
				// Single line list
				listValue := vc.parseListValue(line[strings.Index(line, "[")+1 : strings.LastIndex(line, "]")])
				variables[matches[1]] = TerraformVariable{
					Name:  matches[1],
					Value: listValue,
					Type:  "list",
				}
			} else {
				// Multi-line list
				inList = true
				currentListVar = matches[1]
				listContent = []string{line}
			}
		} else if matches := mapStartPattern.FindStringSubmatch(line); matches != nil {
			// Start of map
			if strings.Contains(line, "}") {
				// Single line map
				mapValue := vc.parseMapValue(line[strings.Index(line, "{")+1 : strings.LastIndex(line, "}")])
				variables[matches[1]] = TerraformVariable{
					Name:  matches[1],
					Value: mapValue,
					Type:  "map",
				}
			} else {
				// Multi-line map
				inMap = true
				currentMapVar = matches[1]
				mapContent = []string{line}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return variables, nil
}

// parseListValue parses a list value from tfvars
func (vc *VariableCompiler) parseListValue(content string) []interface{} {
	content = strings.TrimSpace(content)
	if content == "" {
		return []interface{}{}
	}

	var result []interface{}
	items := strings.Split(content, ",")

	for _, item := range items {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, "\"")
		if item != "" {
			result = append(result, item)
		}
	}

	return result
}

// parseMapValue parses a map value from tfvars
func (vc *VariableCompiler) parseMapValue(content string) map[string]interface{} {
	result := make(map[string]interface{})
	content = strings.TrimSpace(content)

	// Remove opening and closing braces if present
	content = strings.TrimPrefix(content, "{")
	content = strings.TrimSuffix(content, "}")
	content = strings.TrimSpace(content)

	if content == "" {
		return result
	}

	// Split by lines and parse each key-value pair
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse different types: key = "value", key = true/false, key = number
		stringPattern := regexp.MustCompile(`^(\w+)\s*=\s*"([^"]*)"`)
		boolPattern := regexp.MustCompile(`^(\w+)\s*=\s*(true|false)`)
		numberPattern := regexp.MustCompile(`^(\w+)\s*=\s*(\d+(?:\.\d+)?)`)

		if matches := stringPattern.FindStringSubmatch(line); matches != nil && len(matches) >= 3 {
			result[matches[1]] = matches[2]
		} else if matches := boolPattern.FindStringSubmatch(line); matches != nil && len(matches) >= 3 {
			result[matches[1]] = matches[2] == "true"
		} else if matches := numberPattern.FindStringSubmatch(line); matches != nil && len(matches) >= 3 {
			if value, err := strconv.ParseFloat(matches[2], 64); err == nil {
				result[matches[1]] = value
			}
		}
	}

	return result
}

// generateCompiledTfvars generates the final compiled tfvars content
func (vc *VariableCompiler) generateCompiledTfvars() string {
	var content strings.Builder

	content.WriteString("# Compiled variables from multiple tfvars files\n")
	content.WriteString("# Generated by tf-go variable compiler\n\n")

	// Sort variables by name for consistent output
	var names []string
	for name := range vc.variables {
		names = append(names, name)
	}

	// Simple sort
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

	for _, name := range names {
		variable := vc.variables[name]
		content.WriteString(vc.formatVariable(variable))
		content.WriteString("\n")
	}

	return content.String()
}

// formatVariable formats a variable for tfvars output
func (vc *VariableCompiler) formatVariable(variable TerraformVariable) string {
	switch variable.Type {
	case "string":
		return fmt.Sprintf(`%s = "%v"`, variable.Name, variable.Value)
	case "number":
		return fmt.Sprintf(`%s = %v`, variable.Name, variable.Value)
	case "bool":
		return fmt.Sprintf(`%s = %v`, variable.Name, variable.Value)
	case "list":
		if list, ok := variable.Value.([]interface{}); ok {
			var items []string
			for _, item := range list {
				items = append(items, fmt.Sprintf(`"%v"`, item))
			}
			return fmt.Sprintf(`%s = [%s]`, variable.Name, strings.Join(items, ", "))
		}
		return fmt.Sprintf(`%s = []`, variable.Name)
	case "map":
		if mapVal, ok := variable.Value.(map[string]interface{}); ok {
			var items []string
			for k, v := range mapVal {
				switch val := v.(type) {
				case string:
					items = append(items, fmt.Sprintf(`  %s = "%s"`, k, val))
				case bool:
					items = append(items, fmt.Sprintf(`  %s = %t`, k, val))
				case float64:
					if val == float64(int64(val)) {
						items = append(items, fmt.Sprintf(`  %s = %d`, k, int64(val)))
					} else {
						items = append(items, fmt.Sprintf(`  %s = %g`, k, val))
					}
				default:
					items = append(items, fmt.Sprintf(`  %s = "%v"`, k, val))
				}
			}
			return fmt.Sprintf("%s = {\n%s\n}", variable.Name, strings.Join(items, "\n"))
		}
		return fmt.Sprintf(`%s = {}`, variable.Name)
	default:
		return fmt.Sprintf(`%s = "%v"`, variable.Name, variable.Value)
	}
}

// CompileAndWriteTfvars compiles variables and writes to a file
func CompileAndWriteTfvars(tfvarsFiles []string, outputPath string) error {
	compiler := NewVariableCompiler()

	compiledContent, err := compiler.CompileVariables(tfvarsFiles)
	if err != nil {
		return fmt.Errorf("failed to compile variables: %w", err)
	}

	err = os.WriteFile(outputPath, []byte(compiledContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write compiled tfvars: %w", err)
	}

	fmt.Printf("[DEBUG] Compiled tfvars written to: %s\n", outputPath)
	return nil
}

// CompileWithVariablesTf compiles tfvars files and merges with variables.tf defaults
func CompileWithVariablesTf(tfvarsFiles []string, workDir string, outputPath string) error {
	compiler := NewVariableCompiler()

	// First, parse variables.tf to get default values
	variablesTfPath := filepath.Join(workDir, "variables.tf")
	if _, err := os.Stat(variablesTfPath); err == nil {
		fmt.Printf("[DEBUG] Found variables.tf, parsing defaults\n")
		defaults, err := compiler.parseVariablesTf(variablesTfPath)
		if err != nil {
			fmt.Printf("[WARNING] Failed to parse variables.tf: %v\n", err)
		} else {
			// Add defaults to compiler
			for name, variable := range defaults {
				compiler.variables[name] = variable
				if mapVal, ok := variable.Value.(map[string]interface{}); ok {
					fmt.Printf("[DEBUG] Added default map for variable '%s' with %d fields:\n", name, len(mapVal))
					for k, v := range mapVal {
						fmt.Printf("[DEBUG]   %s.%s = %v (%T)\n", name, k, v, v)
					}
				} else {
					fmt.Printf("[DEBUG] Added default for variable '%s': %v (%T)\n", name, variable.Value, variable.Value)
				}
			}
		}
	} else {
		fmt.Printf("[DEBUG] No variables.tf found at: %s\n", variablesTfPath)
	}

	// Then compile tfvars files (these will override defaults)
	fmt.Printf("[DEBUG] Before compiling tfvars, compiler has %d variables\n", len(compiler.variables))
	for name, variable := range compiler.variables {
		if mapVal, ok := variable.Value.(map[string]interface{}); ok {
			fmt.Printf("[DEBUG] Pre-compile variable '%s' has %d map fields\n", name, len(mapVal))
		} else {
			fmt.Printf("[DEBUG] Pre-compile variable '%s': %v\n", name, variable.Value)
		}
	}
	
	compiledContent, err := compiler.CompileVariables(tfvarsFiles)
	if err != nil {
		return fmt.Errorf("failed to compile variables: %w", err)
	}

	fmt.Printf("[DEBUG] Final compiled content:\n%s\n", compiledContent)
	
	err = os.WriteFile(outputPath, []byte(compiledContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write compiled tfvars: %w", err)
	}

	fmt.Printf("[DEBUG] Compiled tfvars with defaults written to: %s\n", outputPath)
	return nil
}

// CompileWithVariablesTfFromSource compiles tfvars files and merges with variables.tf defaults from source directory
func CompileWithVariablesTfFromSource(tfvarsFiles []string, srcDir string, workDir string, outputPath string) error {
	compiler := NewVariableCompiler()

	// Check if srcDir follows the stack pattern (contains app/stacks/{{stack}})
	isStackPath := strings.Contains(srcDir, "/app/stacks/")
	fmt.Printf("[DEBUG] srcDir: %s, isStackPath: %v\n", srcDir, isStackPath)
	
	if isStackPath {
		// For stack-specific path, look for variables.tf in the same directory
		variablesTfPath := filepath.Join(srcDir, "variables.tf")
		fmt.Printf("[DEBUG] Looking for stack variables.tf at: %s\n", variablesTfPath)
		
		if _, err := os.Stat(variablesTfPath); err == nil {
			fmt.Printf("[DEBUG] Found stack-specific variables.tf\n")
			defaults, err := compiler.parseVariablesTf(variablesTfPath)
			if err != nil {
				fmt.Printf("[WARNING] Failed to parse variables.tf: %v\n", err)
			} else {
				// Add defaults to compiler
				fmt.Printf("[DEBUG] Found %d default variables in variables.tf\n", len(defaults))
				for name, variable := range defaults {
					compiler.variables[name] = variable
					if mapVal, ok := variable.Value.(map[string]interface{}); ok {
						fmt.Printf("[DEBUG] Added stack default map for variable '%s' with %d fields:\n", name, len(mapVal))
						for k, v := range mapVal {
							fmt.Printf("[DEBUG]   %s.%s = %v (%T)\n", name, k, v, v)
						}
					} else {
						fmt.Printf("[DEBUG] Added stack default for variable '%s': %v (%T)\n", name, variable.Value, variable.Value)
					}
				}
			}
		} else {
			fmt.Printf("[DEBUG] No variables.tf found at stack path: %s\n", variablesTfPath)
		}
	} else {
		// For non-stack paths, find all variables.tf files recursively and compile from all of them
		fmt.Printf("[DEBUG] Non-stack path detected, searching for all variables.tf files recursively\n")
		
		// Find TF_PATH root (go up until we find a directory that might contain app/stacks)
		tfPath := findTfPathRoot(srcDir)
		fmt.Printf("[DEBUG] Using TF_PATH root: %s\n", tfPath)
		
		variablesTfPaths := findAllVariablesTfFiles(tfPath)
		fmt.Printf("[DEBUG] Found %d variables.tf files\n", len(variablesTfPaths))
		
		for _, path := range variablesTfPaths {
			fmt.Printf("[DEBUG] Processing variables.tf: %s\n", path)
			defaults, err := compiler.parseVariablesTf(path)
			if err != nil {
				fmt.Printf("[WARNING] Failed to parse %s: %v\n", path, err)
				continue
			}
			
			// Merge defaults from this file
			for name, variable := range defaults {
				if existing, exists := compiler.variables[name]; exists {
					// If both are maps, merge them
					if existingMap, ok := existing.Value.(map[string]interface{}); ok {
						if newMap, ok := variable.Value.(map[string]interface{}); ok {
							mergedMap := make(map[string]interface{})
							for k, v := range existingMap {
								mergedMap[k] = v
							}
							for k, v := range newMap {
								mergedMap[k] = v
							}
							compiler.variables[name] = TerraformVariable{
								Name:  name,
								Value: mergedMap,
								Type:  "map",
							}
							fmt.Printf("[DEBUG] Merged variable '%s' from %s\n", name, path)
							continue
						}
					}
					fmt.Printf("[DEBUG] Overriding variable '%s' from %s\n", name, path)
				} else {
					fmt.Printf("[DEBUG] Added variable '%s' from %s\n", name, path)
				}
				compiler.variables[name] = variable
			}
		}
	}

	// Then compile tfvars files (these will override defaults)
	fmt.Printf("[DEBUG] Before compiling tfvars, compiler has %d variables\n", len(compiler.variables))
	for name, variable := range compiler.variables {
		if mapVal, ok := variable.Value.(map[string]interface{}); ok {
			fmt.Printf("[DEBUG] Pre-compile variable '%s' has %d map fields\n", name, len(mapVal))
		} else {
			fmt.Printf("[DEBUG] Pre-compile variable '%s': %v\n", name, variable.Value)
		}
	}
	
	compiledContent, err := compiler.CompileVariables(tfvarsFiles)
	if err != nil {
		return fmt.Errorf("failed to compile variables: %w", err)
	}

	fmt.Printf("[DEBUG] Final compiled content:\n%s\n", compiledContent)
	
	err = os.WriteFile(outputPath, []byte(compiledContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write compiled tfvars: %w", err)
	}

	fmt.Printf("[DEBUG] Compiled tfvars with defaults written to: %s\n", outputPath)
	return nil
}

// parseVariablesTf parses variables.tf to extract default values
func (vc *VariableCompiler) parseVariablesTf(filename string) (map[string]TerraformVariable, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	variables := make(map[string]TerraformVariable)
	scanner := bufio.NewScanner(file)

	var currentVar string
	var inVariable bool
	var inDefault bool
	var inType bool
	var defaultContent []string
	var typeContent []string
	var braceCount int

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		// Start of variable block
		if strings.HasPrefix(line, "variable ") {
			// Extract variable name
			varPattern := regexp.MustCompile(`variable\s+"([^"]+)"`)
			if matches := varPattern.FindStringSubmatch(line); matches != nil {
				currentVar = matches[1]
				inVariable = true
				braceCount = 0
				fmt.Printf("[DEBUG] Found variable definition: %s\n", currentVar)
			}
			continue
		}

		if inVariable {
			// Count braces to track nesting
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")
			

			// Look for default block
			if strings.Contains(line, "default") && strings.Contains(line, "=") && !inType {
				inDefault = true
				defaultContent = []string{}
				fmt.Printf("[DEBUG] Found default line for %s: %s\n", currentVar, line)

				// Handle simple defaults on same line
				if strings.Contains(line, "{}") {
					// default = {}
					variables[currentVar] = TerraformVariable{
						Name:  currentVar,
						Value: make(map[string]interface{}),
						Type:  "map",
					}
					inDefault = false
					continue
				} else if strings.Contains(line, "\"") {
					// default = "value"
					defaultPattern := regexp.MustCompile(`default\s*=\s*"([^"]*)"`)
					if matches := defaultPattern.FindStringSubmatch(line); matches != nil {
						variables[currentVar] = TerraformVariable{
							Name:  currentVar,
							Value: matches[1],
							Type:  "string",
						}
						inDefault = false
						continue
					}
				} else if strings.Contains(line, "true") || strings.Contains(line, "false") {
					// default = true/false
					defaultPattern := regexp.MustCompile(`default\s*=\s*(true|false)`)
					if matches := defaultPattern.FindStringSubmatch(line); matches != nil {
						variables[currentVar] = TerraformVariable{
							Name:  currentVar,
							Value: matches[1] == "true",
							Type:  "bool",
						}
						inDefault = false
						continue
					}
				}

				defaultContent = append(defaultContent, line)
				continue
			}

			// Look for type block (to extract optional defaults) - only for object types
			if strings.Contains(line, "type") && strings.Contains(line, "=") && strings.Contains(line, "object(") && !inDefault {
				inType = true
				typeContent = []string{}
				typeContent = append(typeContent, line)
				continue
			}

			// Continue collecting default or type content
			if inDefault {
				defaultContent = append(defaultContent, line)
				// End of default block - parse complex defaults
				if braceCount <= 1 && (strings.Contains(line, "}") || (!strings.Contains(line, "{") && !strings.Contains(line, "="))) {
					inDefault = false
					// Try to parse the complex default content
					fmt.Printf("[DEBUG] Parsing complex default for variable '%s':\n%s\n", currentVar, strings.Join(defaultContent, "\n"))
					
					// Simple parsing for map defaults
					defaultMap := make(map[string]interface{})
					for _, defaultLine := range defaultContent {
						if strings.Contains(defaultLine, "=") && !strings.Contains(defaultLine, "default") {
							// Try to parse key = value pairs
							parts := strings.SplitN(strings.TrimSpace(defaultLine), "=", 2)
							if len(parts) == 2 {
								key := strings.TrimSpace(parts[0])
								value := strings.TrimSpace(parts[1])
								// Remove quotes and parse basic types
								if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
									defaultMap[key] = strings.Trim(value, "\"")
								} else if value == "true" || value == "false" {
									defaultMap[key] = value == "true"
								} else if strings.Contains(value, ".") {
									if f, err := strconv.ParseFloat(value, 64); err == nil {
										defaultMap[key] = f
									} else {
										defaultMap[key] = value
									}
								} else if i, err := strconv.Atoi(value); err == nil {
									defaultMap[key] = i
								} else {
									defaultMap[key] = strings.Trim(value, "\"")
								}
								fmt.Printf("[DEBUG] Parsed default: %s = %v (%T)\n", key, defaultMap[key], defaultMap[key])
							}
						}
					}
					
					variables[currentVar] = TerraformVariable{
						Name:  currentVar,
						Value: defaultMap,
						Type:  "map",
					}
					fmt.Printf("[DEBUG] Added complex default for '%s' with %d fields\n", currentVar, len(defaultMap))
				}
				continue
			}

			if inType {
				typeContent = append(typeContent, line)
				// End of type block - extract optional defaults
				if braceCount <= 1 && strings.Contains(line, "})") {
					inType = false
					defaults := vc.extractOptionalDefaults(typeContent)
					if len(defaults) > 0 {
						variables[currentVar] = TerraformVariable{
							Name:  currentVar,
							Value: defaults,
							Type:  "map",
						}
						fmt.Printf("[DEBUG] Extracted optional defaults for %s: %v\n", currentVar, defaults)
					}
				}
				continue
			}

			// End of variable block
			if line == "}" && braceCount == 0 {
				inVariable = false
				inDefault = false
				inType = false
				currentVar = ""
				defaultContent = []string{}
				typeContent = []string{}
			}
		}
	}

	return variables, scanner.Err()
}

// extractOptionalDefaults extracts default values from optional() declarations in object types
func (vc *VariableCompiler) extractOptionalDefaults(typeContent []string) map[string]interface{} {
	defaults := make(map[string]interface{})

	// Join all lines and clean up whitespace
	content := strings.Join(typeContent, "\n")

	fmt.Printf("[DEBUG] Parsing type content:\n%s\n", content)

	// Look for each line with optional()
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "optional(") {
			fmt.Printf("[DEBUG] Processing optional line: %s\n", line)

			// Extract field name
			fieldPattern := regexp.MustCompile(`(\w+)\s*=\s*optional\(`)
			fieldMatches := fieldPattern.FindStringSubmatch(line)
			if fieldMatches == nil || len(fieldMatches) < 2 {
				continue
			}
			fieldName := fieldMatches[1]

			// Find the default value - look for the second parameter in optional(type, default)
			optionalStart := strings.Index(line, "optional(")
			if optionalStart == -1 {
				continue
			}

			// Find the matching closing parenthesis
			parenCount := 0
			var defaultStart, defaultEnd int
			commaFound := false

			for i := optionalStart + 9; i < len(line); i++ {
				char := line[i]
				if char == '(' {
					parenCount++
				} else if char == ')' {
					if parenCount == 0 {
						defaultEnd = i
						break
					}
					parenCount--
				} else if char == ',' && parenCount == 0 && !commaFound {
					defaultStart = i + 1
					commaFound = true
				}
			}

			if !commaFound {
				// No default value specified
				continue
			}

			defaultValue := strings.TrimSpace(line[defaultStart:defaultEnd])
			fmt.Printf("[DEBUG] Extracted default value for %s: '%s'\n", fieldName, defaultValue)

			// Parse the default value
			if defaultValue == "true" || defaultValue == "false" {
				defaults[fieldName] = defaultValue == "true"
			} else if strings.HasPrefix(defaultValue, "\"") && strings.HasSuffix(defaultValue, "\"") {
				// String value
				defaults[fieldName] = strings.Trim(defaultValue, "\"")
			} else if regexp.MustCompile(`^\d+$`).MatchString(defaultValue) {
				// Integer value
				if val, err := strconv.Atoi(defaultValue); err == nil {
					defaults[fieldName] = val
				}
			} else if regexp.MustCompile(`^\d+\.\d+$`).MatchString(defaultValue) {
				// Float value
				if val, err := strconv.ParseFloat(defaultValue, 64); err == nil {
					defaults[fieldName] = val
				}
			}

			fmt.Printf("[DEBUG] Parsed optional default: %s = %v (%T)\n", fieldName, defaults[fieldName], defaults[fieldName])
		}
	}

	return defaults
}

// findTfPathRoot finds the TF_PATH root directory by going up from srcDir
func findTfPathRoot(srcDir string) string {
	// Check if TF_PATH environment variable is set
	if tfPath := os.Getenv("TF_PATH"); tfPath != "" {
		return tfPath
	}
	
	// Otherwise, try to find a reasonable root by looking for common terraform project indicators
	currentDir := srcDir
	for {
		// Check if this directory contains app/stacks (indicating it's likely the root)
		if _, err := os.Stat(filepath.Join(currentDir, "app", "stacks")); err == nil {
			return currentDir
		}
		
		// Check if this directory contains common terraform project files
		commonFiles := []string{"terraform.tf", "provider.tf", "main.tf", "versions.tf"}
		foundTerraformFiles := false
		for _, file := range commonFiles {
			if _, err := os.Stat(filepath.Join(currentDir, file)); err == nil {
				foundTerraformFiles = true
				break
			}
		}
		if foundTerraformFiles {
			return currentDir
		}
		
		parent := filepath.Dir(currentDir)
		if parent == currentDir {
			// Reached filesystem root
			break
		}
		currentDir = parent
	}
	
	// Fallback to the original srcDir
	return srcDir
}

// findAllVariablesTfFiles searches for all variables.tf files recursively in the given directory
func findAllVariablesTfFiles(dir string) []string {
	var results []string
	
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		if info.IsDir() {
			// Skip hidden directories and common non-terraform directories
			if strings.HasPrefix(info.Name(), ".") || 
			   info.Name() == "node_modules" || 
			   info.Name() == ".terraform" ||
			   info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		
		if info.Name() == "variables.tf" {
			results = append(results, path)
		}
		
		return nil
	})
	
	if err != nil {
		fmt.Printf("[DEBUG] Error during recursive search for variables.tf files: %v\n", err)
	}
	
	return results
}
