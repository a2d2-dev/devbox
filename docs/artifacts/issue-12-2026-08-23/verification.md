# Issue #12 对抗式 review 整改验收记录

## 实验目的

验证当前分支生成的 UI bundle 能否正常渲染多用户页面，并记录浏览器错误。

## 实验步骤

1. 执行 `make ui`，将 Vite 构建结果同步到 `pkg/console/dist`。
2. 执行 `go build -o bin/devbox-issue12 ./cmd/devbox`，并通过 supervisor 重启当前 checkout 的 `devbox-issue12` 服务。
3. 使用 agent-browser 打开 `http://10.126.126.12:9097`，通过本地凭据 vault 登录管理员账户。
4. 将 `console-ui/dist` 同步到服务配置的 `/dev/shm/issue-12-ui`，强制 reload，并核对页面实际加载 `index-BcJXJCk2.js`。
5. 读取浏览器 console，并截取当前 bundle 的渲染结果。

## 实验记录

- 当前 bundle 加载后页面空白：`current-bundle-render-failure.png`。
- 页面 console 记录 `ReferenceError: minimizeWindow is not defined`。
- `git show $(git merge-base origin/main HEAD):console-ui/src/App.jsx` 同样引用未定义的 `minimizeWindow` 与 `closeWindow`。本次按整改清单恢复了该 merge-base hunk，没有重新引入被裁决为夹带的快捷键改动。
- 浏览器 UI 验证失败；Go 测试、vitest、构建结果需与该浏览器失败分开报告。
