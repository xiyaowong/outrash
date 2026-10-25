# outrash

一个跨平台的缓存清理小工具：扫描本机各程序留下的可重建缓存，勾选，一次清掉，清掉的下次使用时自动回来。
A cross-platform cache cleaner: it scans the rebuildable caches your tools leave behind, you check, one pass clears them, and everything comes back the next time you use it.

![扫描结果 Screenshot](docs/screenshot.png)

## 为什么是它 Why

- **纯离线。** 扫描与清理全部在本机完成。唯一联网的功能是「关于」页的手动检查更新。
  **Offline.** Scanning and cleaning never touch the network. The one online feature is the manual update check on the About sheet.
- **无后台常驻。** 用时打开，关掉即退。
  **No background process.** Open it when you need it; closing it ends it.
- **不产生杂物。** 无配置文件、无注册表、无日志。
  **Leaves nothing behind.** No config files, no registry, no logs.
- **没有广告。** 窗口里只有扫描结果。
  **No ads.** The window holds nothing but the scan results.

## 它做什么 What it does

1. **扫描**：按各工具的环境变量与默认位置逐个目录统计，读不到的位置如实标出。
   **Scan**: measures each tool's cache where its environment variables and default locations say it lives, and flags what it could not read.
2. **挑选**：风险提示、预设与建议泡泡帮忙勾选，也可以只看某个盘、只勾大头。
   **Pick**: risk tags, presets and suggestion bubbles narrow the list; you can also look at one drive only.
3. **清理**：一次按钮，逐项显示进度，每个缓存怎么回来写得清楚。
   **Clean**: one button, per-item progress, and next to every path the sentence that says how it comes back.

界面四页：概览、扫描结果、清理记录、规则。界面语言中文、English 可随时切换。
Four pages: Overview, Scan results, Cleanups, Rules. The interface speaks Chinese and English, switchable any time.

## 它不会做的事 What it will not do

- **列表就是计划。** 勾上的就是会被删的路径，没有第二份清单。
  **The list is the plan.** The checked paths are the paths that get removed; there is no second list.
- **只清能重新拿回来的。** 源码、项目目录、控制台一律不碰。
  **Only what can be fetched back.** Sources, projects and consoles are never touched.
- **不写任何东西。** 「清理记录」只存在内存里，关窗即消失。
  **Writes nothing.** The cleanup log lives in memory and dies with the window.
- **不自动更新。** 只有在「关于」页手动点「检查更新」时才检查更新，不会自动检查、不会自动下载。
  **No automatic updates.** Updates are only checked when you press 检查更新 on the About sheet; the app never checks or downloads on its own.

收录范围是各类软件会自行重建的缓存，清单在应用的「规则」页随时可见。
The catalog covers what software rebuilds on its own; it is on the app's Rules page.

## 构建与运行 Build

```sh
just build       # 输出到 build/ · into build/
just debug       # 不删除，打印清理计划 · deletes nothing, prints the plan
go run .         # 直接跑 · run as-is
```

## 许可 Licence

MIT，见 [LICENSE](LICENSE)。 · MIT, see [LICENSE](LICENSE).
