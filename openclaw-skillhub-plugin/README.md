# OpenClaw SkillHub Plugin

This OpenClaw plugin injects the canonical SkillHub runtime guidance during `before_prompt_build`.
It also includes a bundle MCP declaration for the hosted SkillHub server.

The plugin injects the packaged guidance from:

```text
./guidance.md
```

## Install

Install from a local plugin directory:

```bash
openclaw plugins install ./openclaw-skillhub-plugin
openclaw plugins inspect skillhub --runtime
```

Restart the OpenClaw gateway after installation.

## Uninstall

```bash
openclaw plugins uninstall skillhub
```

Restart the OpenClaw gateway after uninstalling.

## Test

```bash
npm test
```
