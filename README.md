# outrash

扫描本机开发工具的缓存目录，列出可以重新下载或重新编译的内容。扫描落在一个推荐勾选上，你在列表上调整后按“开始清理”。

![扫描结果](docs/screenshot.png)

左侧是概览 / 扫描结果 / 清理记录 / 规则四页，中间是工具、路径、数量、大小、风险五列的表，右侧汇总可清理总量、已选数量和空间。窗口窄到放不下时右侧面板让位，列表不被挤压。

## 安全约定

- 列表本身就是计划：勾选的项就是会被删除的路径，界面上没有第二处清单。
- 只有扫描时量到的、位于 `internal/core/rules.json` 缓存根之下的路径才可能被勾选，且不能等于缓存根本身。
- 盘符根和盘符下一级目录一律拒绝。缓存根本身是链接时整项不扫描；计划项本身、或它到缓存根之间的任何一层是链接时一律拒绝，所以链接不会把删除带到根之外。
- 主目录本身不会被列为计划项。
- 列表里只出现各工具能自行重新下载或重新编译的内容。
- 不写配置、注册表、日志或历史；「清理记录」只存在内存里，关闭窗口即消失。

## 构建与运行

```sh
just build       # 精简构建（-s -w -trimpath），约 9.9 MB，输出到 build/
just release     # mygo 打包构建，带版本号与应用标识
just debug       # 不删除：按“查看清理计划”列出路径、大小和顺序，同时打印到 stdout
just safe        # 正常界面，但拒绝执行任何删除
go run .         # 打开窗口，扫描后按工具分组显示
```

`-debug` 隐含 `-no-delete`。`just ci` 跑 build、vet 和测试；`just lint`、`just race`、`just cover` 分别跑 mygo 的 UI 生命周期检查、竞态检测和覆盖率。`just build` 与 `just build-full` 的差别就是 `-s -w -trimpath` 省下的体积（14.2 MB → 9.9 MB）。Windows 上三个可运行产物都是 GUI 子系统——`build`、`build-full` 自己传 `-H=windowsgui`，`release` 由 mygo 传——双击都不会弹出控制台窗口。

## 添加一个缓存

规则都在 `internal/core/rules.json`，通过 `go:embed` 在构建时打包。在分组里加一个 item：`id`、`name` 必填；分组本身只需要 `id`、`name`；`env` 是优先读取的环境变量；`paths` 按 `windows`/`darwin`/`linux` 给出路径模板，支持 `~` 和 `%VAR%`；`unit` 为 `entries`（默认，根的每个子项算一项）或 `versioned`（根/包/版本，可执行“保留最新”）；`exclude` 列出根下同住但**不可重建**的子目录（如 Poetry 的 `virtualenvs`），它们既不进列表也不计入大小。可选的 `probe` 会在环境变量未设置且默认路径都不存在时调用工具查询自己的缓存位置。

## 开发

```sh
go test ./...
go tool mygo vet .
```

## 许可

MIT，见 [LICENSE](LICENSE)。
