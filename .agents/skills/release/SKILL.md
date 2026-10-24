---
name: release
description: 发一个新版本：定版本号、更新资源版本、打 tag，CI 自动构建并上传产物。
---

# 发版

1. **前置**：main 的 CI 绿、工作区干净、与 origin/main 同步。
2. **版本号**：semver，默认取最新 tag 递增；用户指定则用用户的。
3. **资源版本**：`$env:OUTRASH_VERSION = "<版本>"; just icon` 更新 Windows 资源里的版本号，经 PR 合入（走 task-loop 流程）。
4. **打 tag**：`git tag v<版本> && git push origin v<版本>` —— Release workflow 自动交叉编译 windows/linux/darwin（amd64+arm64）的二进制与对应 zip，连同自动生成的 release notes 一起上传到 Release。
5. **校验**：`gh run watch` 等构建结束，`gh release view v<版本>` 确认 10 个产物齐全（5 个目标 × 二进制 + zip）。
6. **失败回退**：删除 tag 与 Release 修复后重打，或改用新版本号。
