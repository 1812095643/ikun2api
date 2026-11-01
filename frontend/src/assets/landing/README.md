# 首页资源

- 定稿：用户于 2026-10-31 选择的 prism-gate，明暗主题共用。
- prism-gate.webp：从该定稿移除 UI 的背景，Sunburst / max，一次编辑生成，1672×941；网页版 WebP quality 94。
- 六个模型 Logo：Lobe Icons static PNG 2.5.0，深色画面版本；品牌归各自权利人所有。按最大显示尺寸（36px，3 倍屏）缩放为 112×112，减少解码内存。
- Noto Sans SC：Google Fonts 的正文/标题字符子集（WOFF2），400/500/600/700，覆盖 `home.prism` 中文文案与顶栏入口用字，SIL OFL 1.1，见 NotoSansSC-LICENSE.txt；请求记录见 font-source.css.txt。修改首页中文文案后需按同一字符集重新生成，否则新字会回退到系统字体。页面字体与图片均本地加载。
- 箭头、主题图标复用站内 Icon 组件。

- prism-gate-far.webp：原场景清除最近门框和两根前景侧柱后补全的后景，用于镜头推进时解除遮挡。
- 渲染采用原图投影门框层、平滑远景和独立透视地面，固定相机视角沿纵深推进；实验深度图因边缘不精确未用于最终产物。
