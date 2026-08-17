# docs/analysis/ — 项目分析报告

本目录存放项目的各类分析报告。每份报告针对特定主题（工程实践复盘、代码量审计、技术债评估等），供项目复盘和后续改进参考。

## 已有报告

| 文件 | 主题 | 适用场景 |
|------|------|----------|
| `2026-07-29-ai-agent-engineering-practice.md` | AI Agent 软件工程实践复盘 | 理解 AI 驱动开发的流程特征、卡点和产出规模 |
| `2026-08-13-multi-machine-project-collaboration.md` | 多机协作项目的协作边界与冲突面 | 评估多台机器上同时运行 Sift 的可行性 |
| `2026-08-17-continuous-project-orchestration.md` | 持续推进项目的编排需求评估与分期建议 | 判断「外部指挥 + Sift 执行」是否够用、Sift 内置编排是否到立项时机 |
| `project-code-audit-prompt.md` | **代码量/过度工程审计提示词** | 复制到其他项目生成类似分析 |

## 使用方式

- 分析报告绑定基线 commit SHA，可复算
- 引用外部数据（GitHub 标签、全局 skill 版本）时声明可变性
