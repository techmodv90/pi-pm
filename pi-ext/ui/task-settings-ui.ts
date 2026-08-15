import { DynamicBorder } from "@mariozechner/pi-coding-agent";
import { Container, matchesKey, Spacer, Text } from "@mariozechner/pi-tui";

// ── Task Settings Component ────────────────────────────────────

export class TaskSettingsComponent {
  private theme: any;
  private done: () => void;
  private container: Container;

  constructor(tui: any, theme: any, done: () => void) {
    void tui;
    this.theme = theme;
    this.done = done;
    this.container = new Container();
    this.renderView();
  }

  /**
   * Render task-system settings text.
   * Expects no input and rebuilds the component with model switching disabled.
   */
  private renderView() {
    this.container.clear();
    const th = this.theme;
    this.container.addChild(new DynamicBorder((s: string) => th.fg("accent", s)));
    this.container.addChild(new Text(th.fg("accent", th.bold(" Task System Settings")), 1, 0));
    this.container.addChild(new Spacer(1));
    this.container.addChild(new Text(th.fg("text", "  Model switching is disabled."), 1, 0));
    this.container.addChild(new Text(th.fg("dim", "  Work prompts and review prompts stay in the current pi session/model."), 1, 0));
    this.container.addChild(new Spacer(1));
    this.container.addChild(new Text(th.fg("text", "  Review flow:"), 1, 0));
    this.container.addChild(new Text(th.fg("dim", "    task_manager trigger_work_item_review -> task-reviewer -> scheduler verdict"), 1, 0));
    this.container.addChild(new Spacer(1));
    this.container.addChild(new Text(th.fg("dim", "  esc: back"), 1, 0));
    this.container.addChild(new DynamicBorder((s: string) => th.fg("accent", s)));
  }

  /**
   * Handle settings input.
   * Expects keyboard data and exits on escape or ctrl+c.
   */
  handleInput(data: string): void {
    if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) this.done();
  }

  /**
   * Render the settings component.
   * Expects a terminal width and returns rendered lines.
   */
  render(width: number): string[] {
    return this.container.render(width);
  }

  /**
   * Rebuild the settings view.
   * Expects no input and refreshes static settings content.
   */
  invalidate(): void {
    this.renderView();
  }
}
