package opencode

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "os/exec"
 "path/filepath"
 "reflect"
 "sort"
 "strings"
 "time"

 "gopkg.in/yaml.v3"

 "github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/processenv"
 "github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
)

const backboneRuntimeVersion = "1.19.0"
const backboneAgentName = "ao-default"

type backbonePermissionRule struct {
 Permission string `json:"permission"`
 Pattern string `json:"pattern"`
 Action string `json:"action"`
}

// BackboneNativeConfig is the exact process configuration checked before ACP.
// The policy and source checks run before calling this native readback boundary.
type BackboneNativeConfig struct {
 Binary, WorkspacePath, SystemPrompt, Model, Effort string
 Env map[string]string
 Content string
}

type backboneProjectConfig struct {
 Schema string `json:"$schema"`
 Instructions []string `json:"instructions"`
 Permission map[string]json.RawMessage `json:"permission"`
}

func backboneProject(data []byte, documents map[string][]byte) (backboneProjectConfig, error) {
 var project backboneProjectConfig
 if err := backbone.DecodeStrict(data, &project); err != nil { return project, fmt.Errorf("OpenCode Backbone: invalid native project config") }
 if project.Schema != "https://opencode.ai/config.json" || len(project.Instructions) == 0 || len(project.Instructions) > 64 {
  return project, fmt.Errorf("OpenCode Backbone: invalid native project instructions")
 }
 for _, instruction := range project.Instructions {
  if _, ok := documents[instruction]; !ok || strings.ContainsAny(instruction, "{}*?[]\\:") || filepath.IsAbs(instruction) {
   return project, fmt.Errorf("OpenCode Backbone: unbound native project instruction")
  }
 }
 return project, nil
}

// PrepareBackboneACPConfigContent projects the approved implementer role onto
// the primary actually selected by AO. Explicit project denies remain stronger.
func PrepareBackboneACPConfigContent(project, role []byte, documents map[string][]byte, systemPrompt, model, effort, dataHome string) (string, error) {
 config, err := backboneProject(project, documents)
 if err != nil { return "", err }
 permission, err := backboneRolePermission(role)
 if err != nil { return "", err }
 for key, raw := range config.Permission {
  if key == "*" { if string(raw) != `"deny"` { return "", fmt.Errorf("OpenCode Backbone: native wildcard must deny") }; continue }
  value, err := backboneProjectPermission(key, raw)
  if err != nil { return "", err }
  if value == "deny" { permission[key] = "deny"; continue }
  if patterns, ok := value.(map[string]string); ok {
   base, exists := permission[key].(string)
   if !exists { return "", fmt.Errorf("OpenCode Backbone: unsupported native permission") }
   patterns["*"] = stricterAction(base, patterns["*"])
   permission[key] = patterns
  }
 }
 if _, ok := config.Permission["*"]; !ok { return "", fmt.Errorf("OpenCode Backbone: missing native wildcard deny") }
 // Native 1.19 appends a tool-output exception unless this exact path is denied.
 permission["external_directory"] = map[string]string{"*":"deny", filepath.Join(dataHome,"opencode","tool-output","*"):"deny"}
 permission["task"] = "deny"
 overlay := map[string]any{
  "$schema":"https://opencode.ai/config.json", "default_agent":backboneAgentName, "model":model,
  "agent":map[string]any{backboneAgentName:map[string]any{"mode":"primary","prompt":systemPrompt,"model":model,"variant":effort,"permission":permission}},
 }
 data, err := json.Marshal(overlay)
 if err != nil { return "", fmt.Errorf("OpenCode Backbone: encode native configuration") }
 // ConfigVariable expands raw JSON before parsing. Encode the opening brace
 // inside string tokens so document text is delivered literally, not expanded.
 content := strings.ReplaceAll(string(data), "{env:", `\u007benv:`)
 content = strings.ReplaceAll(content, "{file:", `\u007bfile:`)
 return content, nil
}

func stricterAction(a,b string) string {
 if a == "deny" || b == "deny" { return "deny" }
 if a == "ask" || b == "ask" { return "ask" }
 return "allow"
}

func backboneProjectPermission(key string, raw json.RawMessage) (any,error) {
 switch key { case "read","glob","grep","skill","edit","bash": default: return nil,fmt.Errorf("OpenCode Backbone: unsupported project permission") }
 var scalar string
 if json.Unmarshal(raw,&scalar)==nil {
  if scalar != "deny" && scalar != "ask" && scalar != "allow" { return nil,fmt.Errorf("OpenCode Backbone: invalid permission action") }
  if (key=="edit" || key=="bash") && scalar=="allow" { return nil,fmt.Errorf("OpenCode Backbone: write or shell allow is not qualified") }
  return scalar,nil
 }
 var patterns map[string]string
 if err:=backbone.DecodeStrict(raw,&patterns); err!=nil || len(patterns)==0 || len(patterns)>128 { return nil,fmt.Errorf("OpenCode Backbone: invalid permission patterns") }
 for pattern,action:=range patterns {
  if pattern=="" || strings.ContainsAny(pattern,"{}\\\x00") || strings.HasPrefix(pattern,"~") || strings.Contains(pattern,"$HOME") || (action!="deny" && action!="ask" && action!="allow") || ((key=="edit"||key=="bash")&&action=="allow") { return nil,fmt.Errorf("OpenCode Backbone: unsupported permission pattern") }
 }
 return patterns,nil
}

func backboneRolePermission(data []byte) (map[string]any,error) {
 parts:=bytes.SplitN(data,[]byte("\n---"),2)
 if !bytes.HasPrefix(data,[]byte("---\n")) || len(parts)!=2 { return nil,fmt.Errorf("OpenCode Backbone: invalid implementer frontmatter") }
 var document yaml.Node
 if err:=yaml.Unmarshal(parts[0][4:],&document); err!=nil || len(document.Content)!=1 { return nil,fmt.Errorf("OpenCode Backbone: invalid implementer frontmatter") }
 node:=document.Content[0]
 if node.Kind!=yaml.MappingNode || len(node.Content)!=6 { return nil,fmt.Errorf("OpenCode Backbone: unsupported implementer frontmatter") }
 seen:=map[string]bool{}; var permission *yaml.Node
 for i:=0;i<len(node.Content);i+=2 {
  key,value:=node.Content[i],node.Content[i+1]
  if key.Kind!=yaml.ScalarNode || seen[key.Value] || value.Alias!=nil || value.Anchor!="" { return nil,fmt.Errorf("OpenCode Backbone: ambiguous implementer frontmatter") }; seen[key.Value]=true
  switch key.Value { case "description": if value.Kind!=yaml.ScalarNode || value.Tag!="!!str" { return nil,fmt.Errorf("OpenCode Backbone: invalid description") }
  case "mode": if value.Value!="primary" { return nil,fmt.Errorf("OpenCode Backbone: implementer must be primary") }
  case "permission": permission=value
  default: return nil,fmt.Errorf("OpenCode Backbone: unsupported implementer field") }
 }
 if permission==nil || permission.Kind!=yaml.MappingNode || len(permission.Content)!=14 { return nil,fmt.Errorf("OpenCode Backbone: incomplete implementer permissions") }
 result:=map[string]any{}
 for i:=0;i<len(permission.Content);i+=2 {
  key,value:=permission.Content[i],permission.Content[i+1]
  if key.Kind!=yaml.ScalarNode || value.Kind!=yaml.ScalarNode || key.Alias!=nil || value.Alias!=nil || key.Anchor!="" || value.Anchor!="" || result[key.Value]!=nil { return nil,fmt.Errorf("OpenCode Backbone: ambiguous implementer permissions") }
  switch key.Value {
  case "*": if i!=0 || value.Value!="deny" { return nil,fmt.Errorf("OpenCode Backbone: implementer wildcard must deny first") }
  case "read","glob","grep","skill": if value.Value!="allow" && value.Value!="ask" && value.Value!="deny" { return nil,fmt.Errorf("OpenCode Backbone: invalid read permission") }
  case "edit","bash": if value.Value!="ask" && value.Value!="deny" { return nil,fmt.Errorf("OpenCode Backbone: write and shell require approval") }
  default: return nil,fmt.Errorf("OpenCode Backbone: unknown implementer permission") }
  result[key.Value]=value.Value
 }
 return result,nil
}

func expectedBackboneRules(content string) ([]backbonePermissionRule,error) {
 var overlay struct { Agent map[string]struct { Permission map[string]json.RawMessage `json:"permission"` } `json:"agent"` }
 if err:=json.Unmarshal([]byte(content),&overlay); err!=nil { return nil,err }
 permission:=overlay.Agent[backboneAgentName].Permission
 keys:=make([]string,0,len(permission)); for key:=range permission { keys=append(keys,key) }; sort.Strings(keys)
 var rules []backbonePermissionRule
 for _,key:=range keys {
  var scalar string
  if json.Unmarshal(permission[key],&scalar)==nil { rules=append(rules,backbonePermissionRule{key,"*",scalar});continue }
  var patterns map[string]string
  if err:=json.Unmarshal(permission[key],&patterns);err!=nil{return nil,err}
  names:=make([]string,0,len(patterns));for name:=range patterns{names=append(names,name)};sort.Strings(names)
  for _,name:=range names{rules=append(rules,backbonePermissionRule{key,name,patterns[name]})}
 }
 return rules,nil
}

func verifyBackboneAgent(data []byte,cfg BackboneNativeConfig) error {
 var agent struct { Name string `json:"name"`; Mode string `json:"mode"`; Prompt string `json:"prompt"`; Variant string `json:"variant"`; Model struct { ProviderID string `json:"providerID"`; ModelID string `json:"modelID"` } `json:"model"`; Permission []backbonePermissionRule `json:"permission"` }
 if err:=json.Unmarshal(data,&agent);err!=nil { return fmt.Errorf("OpenCode Backbone: invalid native agent readback") }
 if agent.Name!=backboneAgentName || agent.Mode!="primary" || agent.Prompt!=cfg.SystemPrompt || agent.Model.ProviderID+"/"+agent.Model.ModelID!=cfg.Model || agent.Variant!=cfg.Effort { return fmt.Errorf("OpenCode Backbone: native primary, prompt, model or effort mismatch") }
 rules,err:=expectedBackboneRules(cfg.Content);if err!=nil{return fmt.Errorf("OpenCode Backbone: invalid expected permission rules")}
 last:=-1;for i,rule:=range agent.Permission{if rule.Permission=="*"&&rule.Pattern=="*"{last=i}}
 if last<0 || !reflect.DeepEqual(agent.Permission[last:],rules){return fmt.Errorf("OpenCode Backbone: native effective permissions do not match the approved projection")}
 return nil
}

// VerifyBackboneNative never exposes stdout/stderr (which may contain secrets)
// in an error. It executes only fixed non-inference readback commands.
func VerifyBackboneNative(ctx context.Context,cfg BackboneNativeConfig) error {
 version,err:=backboneProbe(ctx,cfg,"--version");if err!=nil{return err}
 if strings.TrimSpace(string(version))!=backboneRuntimeVersion{return fmt.Errorf("OpenCode Backbone: exact native version 1.19.0 is required")}
 data,err:=backboneProbe(ctx,cfg,"debug","config");if err!=nil{return err}
 var resolved map[string]json.RawMessage
 if err:=json.Unmarshal(data,&resolved);err!=nil{return fmt.Errorf("OpenCode Backbone: invalid native config readback")}
 var primary,model string
 _=json.Unmarshal(resolved["default_agent"],&primary);_=json.Unmarshal(resolved["model"],&model)
 if primary!=backboneAgentName || model!=cfg.Model{return fmt.Errorf("OpenCode Backbone: native default selection mismatch")}
 for _,key:=range []string{"plugin","mcp","tools","mode","command","skills","references","reference"} {
  raw:=resolved[key]
  if len(raw)>0 && string(raw)!="null" && string(raw)!="[]" && string(raw)!="{}" {return fmt.Errorf("OpenCode Backbone: unqualified native configuration")}
 }
 data,err=backboneProbe(ctx,cfg,"debug","agent",backboneAgentName);if err!=nil{return err}
 return verifyBackboneAgent(data,cfg)
}

type boundedProbeOutput struct { bytes.Buffer; limit int }
func (b *boundedProbeOutput) Write(p []byte)(int,error){if len(p)>b.limit-b.Len(){return 0,fmt.Errorf("native probe output exceeds limit")};return b.Buffer.Write(p)}
func backboneProbe(ctx context.Context,cfg BackboneNativeConfig,args ...string)([]byte,error){
 probeCtx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 cmd:=exec.CommandContext(probeCtx,cfg.Binary,args...);cmd.Dir=cfg.WorkspacePath;cmd.Env=processenv.Merge(cfg.Env);cmd.WaitDelay=time.Second
 output:=&boundedProbeOutput{limit:6<<20};cmd.Stdout=output;cmd.Stderr=io.Discard
 if err:=cmd.Run();err!=nil { if probeCtx.Err()!=nil{return nil,fmt.Errorf("OpenCode Backbone: native readback timed out or was cancelled")};return nil,fmt.Errorf("OpenCode Backbone: native readback failed") }
 return output.Bytes(),nil
}
