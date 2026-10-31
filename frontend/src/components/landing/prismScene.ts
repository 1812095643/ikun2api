import {
  BufferGeometry, DoubleSide, Float32BufferAttribute, Mesh, MeshBasicMaterial,
  NoToneMapping, PerspectiveCamera, Scene, Shape, ShapeGeometry, SRGBColorSpace, TextureLoader, WebGLRenderer
} from 'three'

export interface ScenePosition { progress: number; x: number; y: number }
export interface PrismScene { render(position: ScenePosition): void; resize(): void; dispose(): void }

/** Camera-projected source layers: foreground architecture, ground plane, and revealed corridor. */
export async function createPrismScene(canvas: HTMLCanvasElement, source: string, farSource: string): Promise<PrismScene> {
  const renderer = new WebGLRenderer({ canvas, alpha: false, antialias: true, powerPreference: 'low-power' })
  renderer.outputColorSpace = SRGBColorSpace
  renderer.toneMapping = NoToneMapping
  renderer.setClearColor(0x0b192c)
  const scene = new Scene()
  const camera = new PerspectiveCamera(48, 1, .1, 500)
  const loader = new TextureLoader()
  const resources = await Promise.allSettled([loader.loadAsync(source), loader.loadAsync(farSource)])
  if (resources.some(result => result.status === 'rejected')) {
    for (const result of resources) if (result.status === 'fulfilled') result.value.dispose()
    renderer.dispose()
    throw new Error('Landing scene assets unavailable')
  }
  const texture = (resources[0] as PromiseFulfilledResult<Awaited<ReturnType<typeof loader.loadAsync>>>).value
  const farTexture = (resources[1] as PromiseFulfilledResult<Awaited<ReturnType<typeof loader.loadAsync>>>).value
  texture.colorSpace = farTexture.colorSpace = SRGBColorSpace
  texture.anisotropy = farTexture.anisotropy = Math.min(4, renderer.capabilities.getMaxAnisotropy())
  const imageWidth = 1672, imageHeight = 941, imageAspect = imageWidth / imageHeight
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
        uvs.push(u, 1 - v)
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
      uv.setXY(i, u, 1 - v)
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
  let last: ScenePosition = { progress: 0, x: 0, y: 0 }
  function render(position: ScenePosition) {
    if (disposed) return
    last = position
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
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5))
    renderer.setSize(width, height, false)
    render(last)
  }
  resize()
  return { render, resize, dispose() {
    if (disposed) return
    disposed = true
    for (const geometry of geometries) geometry.dispose()
    material.dispose(); farMaterial.dispose(); texture.dispose(); farTexture.dispose()
    renderer.dispose(); renderer.forceContextLoss()
  } }
}
