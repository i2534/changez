# 文件详情页面优化计划

## 优化清单

### P0 - 性能

| # | 优化项 | 当前问题 | 方案 | 涉及文件 |
|---|---|---|---|---|
| 1 | CodeView 虚拟滚动 | 逐行高亮大文件卡顿 | react-window `FixedSizeList` + **On-demand 高亮**（仅对可视行执行 Prism tokenize，滚动时短暂无高亮闪烁可接受） | CodeView.tsx | ✅ |
| 2 | Timeline 虚拟滚动 | 长列表全量渲染 | react-window `VariableSizeList`（条目高度不固定：展开/折叠详情、选中等态改变高度），估算行高 + 测量实际高度 | Timeline.tsx | ✅ |
| 3 | DiffViewer 依赖验证 | useEffect 依赖链需验证完整性 | 当前有效依赖链为 `[diff]`（`fromVersion`/`toVersion` 仅用于 JSX 展示，不参与 draw 逻辑），验证依赖数组是否覆盖所有变化源 | DiffViewer.tsx | ✅ |

### P1 - UX

| # | 优化项 | 当前问题 | 方案 | 涉及文件 |
|---|---|---|---|---|
| 4 | 版本选择优化 | 限制 2 个，无法快速定位 | **单击左侧圆点**切换 diff 选中状态；**Shift+Click** 选中范围，自动取首尾版本做 diff；不支持 3+ 版本叠加 diff（diff2html 不支持） | FileTimeline.tsx, Timeline.tsx | ✅ |
| 5 | 内容查看内联 | 需先选择版本，操作步骤多 | **单击条目**直接内联展开内容（调用 restore API），收起前一个展开内容；**复制按钮放在展开区域内**，无需底部操作区 | FileTimeline.tsx, Timeline.tsx | ✅ |
| 6 | Diff 按钮跟随选中 | 底部操作区离选中条目远，长列表需滚动 | **选中 2 个版本后，两个选中条目右侧均显示 "↔ Diff" 按钮**，点击直接跳转 diff 页面；底部吸底栏保留作为备选入口 | Timeline.tsx | ✅ |
| 7 | Diff 页面导航 | navigate(-1) 可能丢失上下文 | 面包屑导航 + 退路方案（`window.history.length <= 1` 时返回文件列表页） | DiffPage.tsx | ✅ |
| 8 | 版本搜索/跳转 | 无快速定位功能 | 版本号输入框 + 键盘快捷键（Ctrl+G 聚焦输入框） | Timeline.tsx | ✅ |
| 9 | 筛选状态持久化 | 刷新丢失筛选条件 | URL search params 存储（`?source=&action=`） | Timeline.tsx | ✅ |

### P2 - 代码结构

| # | 优化项 | 当前问题 | 方案 | 涉及文件 |
|---|---|---|---|---|
| 10 | 提取公共 Hook | handleViewContent 重复实现 | useFileContent Hook，**参数化错误消息 key**（两处实现几乎一致，仅 i18n key 不同：`diff.corrupted_view` vs `timeline.corrupted_restore`） | 新文件 + FileTimeline.tsx, DiffPage.tsx | ✅ |
| 11 | 拆分 Timeline 职责 | 筛选+渲染耦合 | TimelineFilter + TimelineList 组件 | Timeline.tsx → 新文件 | ✅ |

## 实施顺序

```
Phase 1: 性能基础 ✅
├── #1 CodeView 虚拟滚动（FixedSizeList + On-demand 高亮）
├── #2 Timeline 虚拟滚动（VariableSizeList + 动态高度）
└── #3 DiffViewer 依赖验证

Phase 2: UX 改进 ✅
├── #4 版本选择优化（Shift+Click 范围选择）
├── #5 内容查看内联（复制按钮在展开区域内）
├── #6 Diff 按钮跟随选中（选中条目右侧显示 + 吸底栏备选）
├── #7 Diff 页面导航（面包屑 + 退路）
├── #8 版本搜索/跳转
└── #9 筛选状态持久化

Phase 3: 代码重构 ✅
├── #10 提取公共 Hook（参数化错误消息 key）
└── #11 拆分 Timeline 组件
```

## 依赖

- `react-window` (虚拟滚动)

## 注意事项

- 保持现有 API 调用不变
- 保持 i18n 兼容性
- 保持 Tailwind 样式风格一致
- **虚拟滚动状态管理**：Timeline 的 `expandedId` 状态需与可视区域协同（滚动出可视区域的展开状态保留，不自动收起）；展开/折叠时通过 `useDynamicRowHeight` hook 更新行高缓存，react-window v2 使用 `DynamicRowHeight` 替代 v1 的 `resetAfterIndex`
- **CodeView 高亮策略**：采用 On-demand 方案，滚动时短暂无高亮闪烁为可接受权衡
- **Diff 按钮交互**：选中 2 个版本后，两个选中条目右侧均显示 "↔ Diff" 按钮；底部吸底栏保留作为备选入口，不自动跳转
- **点击交互区分**：单击条目→展开/收起内容，单击左侧圆点→切换 diff 选中，Shift+Click→范围选择
