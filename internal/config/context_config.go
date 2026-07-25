package config

import "gopkg.in/yaml.v3"

func (c *ContextConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawContextConfig ContextConfig

	var decoded rawContextConfig
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*c = ContextConfig(decoded)

	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			switch node.Content[i].Value {
			case "enabled":
				c.enabledSet = true
			case "auto_compact":
				c.autoCompactSet = true
			}
		}
	}
	return nil
}

func (c ContextConfig) EnabledExplicitlySet() bool {
	return c.enabledSet
}
