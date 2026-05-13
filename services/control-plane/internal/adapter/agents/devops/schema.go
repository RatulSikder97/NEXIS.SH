package devops

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["argocd_app_yaml", "gh_actions_yaml", "rollout_strategy"],
  "properties": {
    "argocd_app_yaml":  {"type": "string", "minLength": 10},
    "gh_actions_yaml":  {"type": "string", "minLength": 10},
    "rollout_strategy": {"type": "string", "enum": ["canary-10-50-100", "blue-green", "rolling", "recreate"]}
  }
}`
