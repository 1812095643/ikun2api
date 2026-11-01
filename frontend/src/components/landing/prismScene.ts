import {
  BufferGeometry, DoubleSide, Float32BufferAttribute, ImageBitmapLoader, ImageLoader, Mesh, MeshBasicMaterial,
  NoToneMapping, PerspectiveCamera, Scene, Shape, ShapeGeometry, SRGBColorSpace, Texture, WebGLRenderer
} from 'three'

export interface ScenePosition { progress: number; x: number; y: number }
export interface PrismScene {
  render(position: ScenePosition): void
  /** 只同步画布尺寸与相机比例；尺寸变化会清空画面，调用方需要时再 render。 */
  resize(): void
  /** loseContext 为 true 时立即归还 GPU 上下文（仅在画布不再使用时传入，否则同一画布无法再次创建上下文） */
  dispose(loseContext?: boolean): void
}

type SceneImage = ImageBitmap | HTMLImageElement

const imageWidth = 1672, imageHeight = 941, imageAspect = imageWidth / imageHeight
// 源图只有 1672×941 个纹素：绘制缓冲超过源图纹素密度不会增加细节，只会成倍增加显存和填充开销。
const MAX_PIXEL_RATIO = 1.5

/** 让出主线程一次，把初始化拆成多个短任务。 */
const nextTask = () => new Promise<void>(resolve => setTimeout(resolve))
/** 释放 ImageBitmap 的解码副本；<img> 由浏览器自行管理。 */
const releaseImage = (image: SceneImage) => { if ('close' in image) image.close() }
const abortError = () => new DOMException('Landing scene build aborted', 'AbortError')

/** Camera-projected source layers: foreground architecture, ground plane, and revealed corridor. */
export async function createPrismScene(canvas: HTMLCanvasElement, source: string, farSource: string, signal?: AbortSignal): Promise<PrismScene> {
  const renderer = new WebGLRenderer({ canvas, alpha: false, antialias: true, stencil: false, powerPreference: 'low-power' })
  renderer.outputColorSpace = SRGBColorSpace
  renderer.toneMapping = NoToneMapping
  renderer.setClearColor(0x0b192c)
  const scene = new Scene()
  const camera = new PerspectiveCamera(48, 1, .1, 500)
  // 性能修复：原先 TextureLoader 在首帧渲染时于主线程同步解码两张大图（叠加着色器编译形成约 370ms 长任务）。
  // ImageBitmap 在后台线程解码，上传 GPU 后即释放解码副本。
  const bitmapLoader = new ImageBitmapLoader()
  const imageLoader = new ImageLoader()
  // 不支持 ImageBitmap 的浏览器退回 <img> 解码；两种来源都按 flipY=false 上传，朝向一致。
  const load = (url: string): Promise<SceneImage> => bitmapLoader.loadAsync(url)
    .catch(error => signal?.aborted ? Promise.reject(error) : imageLoader.loadAsync(url))
  // 卸载、减少动态效果或上下文丢失时由调用方中止，立即取消仍在进行的图片请求。
  const stopLoading = () => bitmapLoader.abort()
  signal?.addEventListener('abort', stopLoading, { once: true })
  const resources = await Promise.allSettled([load(source), load(farSource)])
  signal?.removeEventListener('abort', stopLoading)
  if (signal?.aborted || resources.some(result => result.status === 'rejected')) {
    for (const result of resources) if (result.status === 'fulfilled') releaseImage(result.value)
    renderer.dispose()
    // 画布已随组件移出页面时才强制归还上下文，否则同一画布还需重建。
    if (!canvas.isConnected) renderer.forceContextLoss()
    throw signal?.aborted ? abortError() : new Error('Landing scene assets unavailable')
  }
  const [image, farImage] = resources.map(result => (result as PromiseFulfilledResult<SceneImage>).value)
  const anisotropy = Math.min(4, renderer.capabilities.getMaxAnisotropy())
  function sceneTexture(source: SceneImage) {
    // ImageBitmap 按自上而下的行序上传，flipY 不生效；统一关闭 flipY，下方 UV 按该方向生成。
    const texture = new Texture(source)
    texture.flipY = false
    texture.colorSpace = SRGBColorSpace
    texture.anisotropy = anisotropy
    texture.needsUpdate = true
    return texture
  }
  const texture = sceneTexture(image), farTexture = sceneTexture(farImage)
  const tangent = Math.tan(24 * Math.PI / 180)
  const geometries: BufferGeometry[] = []
  const material = new MeshBasicMaterial({ map: texture, side: DoubleSide, toneMapped: false })
  const farMaterial = new MeshBasicMaterial({ map: farTexture, side: DoubleSide, toneMapped: false })

  function projectedSurface(cols: number, rows: number, vStart: number, distance: (u: number, v: number) => number, useFar: boolean) {
    const positions: number[] = [], uvs: number[] = [], indices: number[] = []
    for (let row = 0; row <= rows; row++) {
      for (let col = 0; col <= cols; col++) {
        const u = col / cols, v = vStart + row / rows * (1 - vStart), d = distance(u, v)
        positions.push((u * 2 - 1) * tangent * imageAspect * d, (1 - v * 2) * tangent * d, -d)
        uvs.push(u, v)
        if (row < rows && col < cols) {
          const a = row * (cols + 1) + col, b = a + 1, c = a + cols + 1
          indices.push(a, c, b, b, c, c + 1)
        }
      }
    }
    const geometry = new BufferGeometry()
    geometry.setAttribute('position', new Float32BufferAttribute(positions, 3))
    geometry.setAttribute('uv', new Float32BufferAttribute(uvs, 2))
    geometry.setIndex(indices)
    geometries.push(geometry)
    const mesh = new Mesh(geometry, useFar ? farMaterial : material)
    mesh.frustumCulled = false
    scene.add(mesh)
  }

  // Smooth depth for the unobstructed corridor, with near side architecture and a distant sky.
  projectedSurface(120, 80, 0, (u, v) => {
    const side = Math.pow(Math.abs(u - .5) * 2, 1.8)
    const vertical = Math.max(0, Math.min(1, (v - .32) / .36))
    return 180 / (1 + side * vertical * 7)
  }, true)
  // Perspective ground: close reflections move faster; the horizon remains anchored.
  projectedSurface(120, 110, .746, (_u, v) => Math.min(178, 2.0 / Math.max(.0113, v - .742)), false)

  function architecture(points: [number, number][], distance: number) {
    const shape = new Shape()
    points.forEach(([x, y], index) => index ? shape.lineTo(x, -y) : shape.moveTo(x, -y))
    shape.closePath()
    const geometry = new ShapeGeometry(shape)
    const position = geometry.getAttribute('position')
    const uv = geometry.getAttribute('uv')
    for (let i = 0; i < position.count; i++) {
      const u = position.getX(i) / imageWidth, v = -position.getY(i) / imageHeight
      position.setXYZ(i, (u * 2 - 1) * tangent * imageAspect * distance, (1 - v * 2) * tangent * distance, -distance)
      uv.setXY(i, u, v)
    }
    position.needsUpdate = uv.needsUpdate = true
    geometries.push(geometry)
    const mesh = new Mesh(geometry, material)
    mesh.frustumCulled = false
    scene.add(mesh)
  }
  // These are source-image cut lines, not replacement artwork. Every surface uses the original pixels.
  architecture([[155,243],[226,272],[171,610],[80,584]], 14.5)
  architecture([[1398,336],[1499,299],[1527,327],[1583,579],[1495,627]], 14.5)
  architecture([[329,0],[520,0],[447,412],[429,695],[249,715],[191,677]], 10.1)
  architecture([[486,0],[635,0],[1375,211],[1392,285],[1220,252],[485,42]], 10.1)
  architecture([[1281,183],[1375,211],[1465,703],[1410,723],[1255,708],[1214,253]], 10.1)

  let disposed = false
  function render(position: ScenePosition) {
    if (disposed) return
    const travel = position.progress * 6.15
    camera.position.set(position.x * .08, -.484 * tangent * travel + position.y * .045, -travel)
    renderer.render(scene, camera)
    canvas.dataset.cameraZ = camera.position.z.toFixed(3)
  }
  function resize() {
    if (disposed) return
    const width = Math.max(1, canvas.clientWidth), height = Math.max(1, canvas.clientHeight)
    const aspect = width / height
    camera.aspect = aspect
    camera.fov = 2 * Math.atan(tangent * Math.min(1, imageAspect / aspect)) * 180 / Math.PI
    camera.updateProjectionMatrix()
    // 按 cover 缩放后的纹素密度设置像素比（不低于 1、不超过设备像素比和 1.5）。
    // 1600×900@1.5 时绘制缓冲由 2400×1350 降为约 1672×941，多重采样缓冲显存约减半。
    const texelDensity = Math.min(imageWidth / width, imageHeight / height)
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, MAX_PIXEL_RATIO, Math.max(1, texelDensity)))
    renderer.setSize(width, height, false)
  }
  function dispose(loseContext = false) {
    if (disposed) return
    disposed = true
    for (const geometry of geometries) geometry.dispose()
    material.dispose(); farMaterial.dispose(); texture.dispose(); farTexture.dispose()
    releaseImage(image); releaseImage(farImage)
    const contextLost = renderer.getContext().isContextLost()
    renderer.dispose()
    if (loseContext && !contextLost) renderer.forceContextLoss()
  }
  function throwIfAborted() {
    if (signal?.aborted) throw abortError()
  }

  try {
    resize()
    // 着色器异步编译（KHR_parallel_shader_compile），纹理分两次上传，均不在首帧渲染时阻塞主线程。
    await renderer.compileAsync(scene, camera)
    throwIfAborted()
    renderer.initTexture(texture)
    await nextTask()
    throwIfAborted()
    renderer.initTexture(farTexture)
    // GPU 已持有纹理，释放 CPU 侧解码副本（约 12MB）。上下文丢失后由调用方整体重建。
    releaseImage(image); releaseImage(farImage)
  } catch (error) {
    dispose(!canvas.isConnected)
    throw error
  }
  return { render, resize, dispose }
}
