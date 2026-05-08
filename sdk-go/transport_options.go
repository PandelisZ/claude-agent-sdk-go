package claudeagentsdk

import internaltransport "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/transport"

func toInternalTransportOptions(options ClaudeAgentOptions) internaltransport.Options {
	internalOptions := internaltransport.Options{
		Tools:                    append([]string(nil), options.Tools...),
		AllowedTools:             append([]string(nil), options.AllowedTools...),
		SystemPrompt:             options.SystemPrompt,
		StrictMCPConfig:          options.StrictMCPConfig,
		ContinueConversation:     options.ContinueConversation,
		Resume:                   options.Resume,
		SessionID:                options.SessionID,
		ForkSession:              options.ForkSession,
		MaxTurns:                 options.MaxTurns,
		MaxBudgetUSD:             options.MaxBudgetUSD,
		DisallowedTools:          append([]string(nil), options.DisallowedTools...),
		Model:                    options.Model,
		FallbackModel:            options.FallbackModel,
		PermissionPromptToolName: options.PermissionPromptToolName,
		Cwd:                      options.Cwd,
		CLIPath:                  options.CLIPath,
		Settings:                 options.Settings,
		AddDirs:                  append([]string(nil), options.AddDirs...),
		Env:                      cloneStringMap(options.Env),
		ExtraArgs:                cloneOptionalStringMap(options.ExtraArgs),
		MaxBufferSize:            options.MaxBufferSize,
		Stderr:                   options.Stderr,
		User:                     options.User,
		IncludePartialMessages:   options.IncludePartialMessages,
		IncludeHookEvents:        options.IncludeHookEvents,
		Plugins:                  make([]internaltransport.SDKPluginConfig, 0, len(options.Plugins)),
		Effort:                   options.Effort,
		OutputFormat:             cloneAnyMap(options.OutputFormat),
		EnableFileCheckpointing:  options.EnableFileCheckpointing,
		SessionMirror:            options.SessionStore != nil,
	}

	if options.ToolsPreset != nil {
		internalOptions.ToolsPreset = &internaltransport.ToolsPreset{
			Type:   options.ToolsPreset.Type,
			Preset: options.ToolsPreset.Preset,
		}
	}
	if options.SystemPromptPreset != nil {
		internalOptions.SystemPromptPreset = &internaltransport.SystemPromptPreset{
			Type:   options.SystemPromptPreset.Type,
			Preset: options.SystemPromptPreset.Preset,
			Append: options.SystemPromptPreset.Append,
		}
	}
	if options.SystemPromptFile != nil {
		internalOptions.SystemPromptFile = &internaltransport.SystemPromptFile{
			Type: options.SystemPromptFile.Type,
			Path: options.SystemPromptFile.Path,
		}
	}
	if options.PermissionMode != nil {
		mode := internaltransport.PermissionMode(*options.PermissionMode)
		internalOptions.PermissionMode = &mode
	}
	if len(options.Betas) > 0 {
		internalOptions.Betas = make([]internaltransport.SdkBeta, 0, len(options.Betas))
		for _, beta := range options.Betas {
			internalOptions.Betas = append(internalOptions.Betas, internaltransport.SdkBeta(beta))
		}
	}
	if options.TaskBudget != nil {
		internalOptions.TaskBudget = &internaltransport.TaskBudget{Total: options.TaskBudget.Total}
	}
	if len(options.MCPServers) > 0 {
		internalOptions.MCPServers = make(map[string]internaltransport.MCPServerConfig, len(options.MCPServers))
		for name, config := range options.MCPServers {
			switch typed := config.(type) {
			case MCPStdioServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPStdioServerConfig{
					Type:    typed.Type,
					Command: typed.Command,
					Args:    append([]string(nil), typed.Args...),
					Env:     cloneStringMap(typed.Env),
				}
			case MCPSSEServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPSSEServerConfig{
					Type:    typed.Type,
					URL:     typed.URL,
					Headers: cloneStringMap(typed.Headers),
				}
			case MCPHTTPServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPHTTPServerConfig{
					Type:    typed.Type,
					URL:     typed.URL,
					Headers: cloneStringMap(typed.Headers),
				}
			case MCPSDKServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPSDKServerConfig{
					Type: typed.Type,
					Name: typed.Name,
				}
			}
		}
	}
	if len(options.SettingSources) > 0 {
		internalOptions.SettingSources = make([]internaltransport.SettingSource, 0, len(options.SettingSources))
		for _, source := range options.SettingSources {
			internalOptions.SettingSources = append(internalOptions.SettingSources, internaltransport.SettingSource(source))
		}
	}
	if options.Sandbox != nil {
		internalOptions.Sandbox = &internaltransport.SandboxSettings{
			Enabled:                   options.Sandbox.Enabled,
			AutoAllowBashIfSandboxed:  options.Sandbox.AutoAllowBashIfSandboxed,
			ExcludedCommands:          append([]string(nil), options.Sandbox.ExcludedCommands...),
			AllowUnsandboxedCommands:  options.Sandbox.AllowUnsandboxedCommands,
			EnableWeakerNestedSandbox: options.Sandbox.EnableWeakerNestedSandbox,
		}
		if options.Sandbox.Network != nil {
			internalOptions.Sandbox.Network = &internaltransport.SandboxNetworkConfig{
				AllowedDomains:          append([]string(nil), options.Sandbox.Network.AllowedDomains...),
				DeniedDomains:           append([]string(nil), options.Sandbox.Network.DeniedDomains...),
				AllowManagedDomainsOnly: options.Sandbox.Network.AllowManagedDomainsOnly,
				AllowUnixSockets:        append([]string(nil), options.Sandbox.Network.AllowUnixSockets...),
				AllowAllUnixSockets:     options.Sandbox.Network.AllowAllUnixSockets,
				AllowLocalBinding:       options.Sandbox.Network.AllowLocalBinding,
				AllowMachLookup:         append([]string(nil), options.Sandbox.Network.AllowMachLookup...),
				HTTPProxyPort:           options.Sandbox.Network.HTTPProxyPort,
				SOCKSProxyPort:          options.Sandbox.Network.SOCKSProxyPort,
			}
		}
		if options.Sandbox.IgnoreViolations != nil {
			internalOptions.Sandbox.IgnoreViolations = &internaltransport.SandboxIgnoreViolations{
				File:    append([]string(nil), options.Sandbox.IgnoreViolations.File...),
				Network: append([]string(nil), options.Sandbox.IgnoreViolations.Network...),
			}
		}
	}
	if options.Skills != nil {
		internalOptions.Skills = append([]string(nil), options.Skills...)
	}
	for _, plugin := range options.Plugins {
		internalOptions.Plugins = append(internalOptions.Plugins, internaltransport.SDKPluginConfig{
			Type: plugin.Type,
			Path: plugin.Path,
		})
	}
	if options.Thinking != nil {
		internalOptions.Thinking = &internaltransport.ThinkingConfig{
			Type:         internaltransport.ThinkingConfigType(options.Thinking.Type),
			BudgetTokens: options.Thinking.BudgetTokens,
			Display:      (*internaltransport.ThinkingDisplay)(options.Thinking.Display),
		}
	}
	internalOptions.MaxThinkingTokens = options.MaxThinkingTokens

	return internalOptions
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneOptionalStringMap(values map[string]*string) map[string]*string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]*string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
