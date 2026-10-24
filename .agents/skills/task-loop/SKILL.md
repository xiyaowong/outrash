---
name: task-loop
description: 逐个处理任务：建 issue 留痕、分析、实现、PR 自动合并、确认 CI。
---

# Task loop

1. **建档**：新任务先 `gh issue create`（标题 + 期望行为 + 已有分析结论）；多个任务按用户给出的顺序逐个做。
2. **分析**：需要摸底的任务交给只读 subagent 出报告（结构、方案、改动清单、测试影响），实现自己做。
3. **实现**：`git checkout -b <分支>` → 提交 → push → `gh pr create` → `gh pr merge --auto --squash --delete-branch`。
4. **PR**：只写最终行为与非显然权衡，不提未合并的中间状态，不加署名；关联 issue 用 `Closes #N`。
5. **验证**：提交前 `just ci` + `just lint`（涉并发加 `just race`）；UI 改动用 `OUTRASH_SHOTS=1` 截图目检后移除临时代码；合并后确认 main 的 CI 绿。
6. **反馈**：处理期间留意相关 issue 的新回复。
