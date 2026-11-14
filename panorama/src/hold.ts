// A layout's native changes are released only when the layout unloads, and
// the game's HUD panels persist from game to game. So the mods' drawing, and
// with it every layout, lives only while the stock guard may hold the HUD:
// when the guard gives the HUD back, on a leave, a lapsed lease or in the
// hideout, the drawing is cleared, each layout unloads and releases its
// changes, and once the guard may hold the HUD again the last screen is drawn
// afresh.

import type { Screen } from '../../src/wasm/ui/ui.pb.js'
import { TICK_SECONDS } from './guard.js'

/** HoldHost is the guard's level, the drawing and the clock a hold uses. */
export interface HoldHost {
  /** allowed reports whether the stock guard may hold the game's HUD. */
  allowed(): boolean
  /** draw makes the drawing match screen, as Drawing.draw does. */
  draw(screen: Screen | null): void
  /** Schedule runs callback once after seconds, as $.Schedule does. */
  Schedule(seconds: number, callback: () => void): void
}

/** ScreenHold draws the last screen only while the guard may hold the HUD. */
export class ScreenHold {
  /** last is the newest screen the relay gave. */
  private last: Screen | null = null
  /** drawn is the screen the drawing holds, or null for none. */
  private drawn: Screen | null = null
  /** ticking records that a check is scheduled. */
  private ticking = false

  constructor(private readonly host: HoldHost) {}

  /** show takes a screen after the guard took it, or null for none. */
  show(screen: Screen | null): void {
    this.last = screen
    this.check()
  }

  /**
   * check draws the last screen if the guard may hold the HUD and clears the
   * drawing if it may not, and checks again while anything is drawn: the
   * lease lapses and the hideout comes with no event.
   */
  check(): void {
    const want = this.host.allowed() ? this.last : null
    if (want !== this.drawn) {
      this.drawn = want
      this.host.draw(want)
    }
    if (this.drawn !== null && !this.ticking) {
      this.ticking = true
      this.host.Schedule(TICK_SECONDS, () => {
        this.ticking = false
        this.check()
      })
    }
  }
}
