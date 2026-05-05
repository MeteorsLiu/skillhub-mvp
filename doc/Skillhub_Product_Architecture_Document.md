# SkillHub 产品定义文档（PDD）

> 版本：重写版 v4
>
> 更新时间：2026-04-17

## 1. 执行摘要

`SkillHub` 要解决的，不是“怎么再造一个 agent”，而是“怎么把可分发、可治理、可复用的 `SKILL.md` 能力按需交给宿主 Agent 使用”。

当前用户的真实问题是：

1. 找不到合适的 skill。
2. 装了也不一定触发。
3. 装了也不一定能用。
4. 不知道当前结果用了哪个 skill、哪个版本。
5. 不敢信来源，不敢给权限。
6. 同一套能力很难跨宿主、跨机器复用。

因此，SkillHub 的产品形态应定义为：

`一个面向宿主 Agent 的云端 skill 中心 + 本地状态与准备层。`

这里的关键架构决策是：

- **项目入口是 `SKILL.md`，不是 MCP。**
- **模型通过宿主原生 tool 请求 SkillHub 服务，不需要额外引入 MCP 作为第一入口。**
- **SkillHub 自己持有能力索引、版本、缓存、readiness 和信任状态，而不是把这些信息全塞进模型上下文。**

它统一管理：

- `Skill`
- `Skill Metadata`
- `Service Tool Contract`
- `Automation`
- `Bundle`
- `Capability Index`
- `Local Cache`
- `Local State`
- `Trust Record`
- `Skill Session`

## 2. 产品定义

### 2.1 一句话定义

`SkillHub = 以 versioned SKILL.md 为入口的 AI 能力解析、分发与治理系统。`

### 2.2 它是什么

- 一个云端 skill registry。
- 一个本地状态、cache 和 readiness 管理层。
- 一个把 `SKILL.md`、metadata、service tool contract、automation 和 bundle 统一组织起来的能力层。
- 对宿主 Agent 而言，一组可按需加载的 skills，加上一组由 skill 引导使用的宿主原生 tools。

## 3. 核心用户痛点

| 用户痛点 | 用户真实感受 |
| --- | --- |
| 找能力难 | “我知道可能有现成 skill，但我不知道搜什么、哪个能用、哪个更靠谱。” |
| 触发不稳定 | “明明装了，为什么这次没用上？” |
| 本地状态混乱 | “明明显示可用，但当前宿主认不到；换个 workspace 或机器又不一致。” |
| 前置条件复杂 | “还要配置登录态、secret、组织开关、权限策略，太麻烦。” |
| 概念不清 | “skill、tool、automation、bundle 到底是什么关系？” |
| 上下文污染 | “能力越多，Agent 越重，触发越乱。” |
| 权限不透明 | “这个能力到底会访问什么、上传什么、需要什么凭证？” |
| 安全不放心 | “我不知道它是不是恶意的，也不知道来源是不是可信。” |
| 复用困难 | “同一套能力在一个宿主能用，在另一个宿主不一定能用。” |

## 4. 核心对象模型

### 4.1 Skill

`Skill` 是云端托管、版本化的 `SKILL.md` 能力资产。

它是模型能直接理解和遵循的入口对象，不是隐藏在 skill 后面的程序本体。

它的职责：

- 描述何时应该使用该 skill。
- 描述该类任务如何拆解和完成。
- 声明需要哪些 tools、前置条件、secret 和副作用边界。
- 告诉模型什么时候应该继续加载下一级 skill，什么时候应该直接调用 tool。

### 4.2 Skill Metadata

`Skill Metadata` 是从 `SKILL.md` front matter 或旁路元数据中提取出来的结构化描述层。

它至少描述：

- 名称与版本
- 摘要与标签
- 适用意图
- 输入输出摘要
- 依赖声明
- 兼容宿主
- 风险与权限提示
- bundle 归属

### 4.3 Service Tool Contract

`Service Tool Contract` 是宿主原生 tools 的稳定契约层。

它回答的问题不是“这个能力走不走 MCP”，而是：

- 模型可以调用哪些 tool
- 每个 tool 的输入输出 schema 是什么
- 哪些 tool 背后会请求 SkillHub 服务
- 哪些 tool 会产生副作用

MCP 最多只是某些宿主里可选的一种传输适配，不应成为 SkillHub 的核心对象。

### 4.4 Automation

`Automation` 是不需要模型做语义判断的确定性步骤，例如预处理、格式转换、固定脚本或服务端 workflow。

### 4.5 Bundle

`Bundle = SKILL.md + Metadata + Tool Contract + Automation + Dependency + Host Policy`

Bundle 是真正的发布单元和版本单元。

用户拿到的不是单个 skill 文件，而是一套可以被搜索、解析、加载、校验和审计的能力资产。

### 4.6 Bundle 结构

推荐 bundle 用一个清晰的清单描述最小发布单元：

```yaml
apiVersion: skillhub.dev/v1
kind: SkillBundle
metadata:
  name: social/xiaohongshu
  version: 1.4.2
  channel: stable
  publisher: acme-labs

spec:
  summary: 小红书内容生产与发布套件
  entrySkill: publish-post

  skills:
    - name: read-latest-posts
      path: skills/read-latest-posts/SKILL.md
    - name: draft-post
      path: skills/draft-post/SKILL.md
    - name: publish-post
      path: skills/publish-post/SKILL.md

  toolContracts:
    - name: skillhub_search
      version: v1
    - name: skillhub_load
      version: v1
    - name: xiaohongshu_publish
      version: v1

  automations:
    - name: normalize-images
      action: media.normalize_images

  dependencies:
    bundles:
      - name: media/image-tools
        version: "2.1.0"
    secrets:
      - name: XHS_SESSION_TOKEN
        scope: skill
        skill: publish-post

  hostPolicy:
    allowDomains:
      - xiaohongshu.com
      - edith.xiaohongshu.com
    sideEffects: true
```
