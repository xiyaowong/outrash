---
name: release
description: 发一个新版本：定版本号、一次版本提交、打 tag，CI 自动构建并上传产物。
---

# 发版

1. **前置**：main 的 CI 绿、工作区干净、与 origin/main 同步。
2. **版本号**：semver，默认取最新 tag 递增；用户指定则用用户的。
3. **版本提交**：`mygo.json` 的 `version` 递增，一个提交，消息即版本（`0.3.0`），经 PR 合入。没有别的发版提交。
4. **打 tag**：`git tag v<版本> && git push origin v<版本>` —— Release workflow 以 tag 为准写入各产物的版本并自动构建上传。
5. **校验**：`gh run watch` 等构建结束，`gh release view v<版本>` 确认产物齐全且 Release 已发布。
6. **失败回退**：删除 tag 与 Release 修复后重打，或改用新版本号。
