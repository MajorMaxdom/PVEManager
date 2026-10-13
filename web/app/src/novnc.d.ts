declare module '@novnc/novnc' {
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, url: string, options?: { credentials?: { password?: string }; shared?: boolean; wsProtocols?: string[] })
    scaleViewport: boolean
    resizeSession: boolean
    clipViewport: boolean
    focusOnClick: boolean
    sendCtrlAltDel(): void
    focus(): void
    disconnect(): void
  }
}
