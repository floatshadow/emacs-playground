/** What a scenario's mode and tags ask of the session (CURRICULUM.md B.3). */
import { TagGUI, TagRemote, type Scenario } from '../../protocol.gen'

/** GUI lessons: `mode: gui` or tagged gui (CURRICULUM.md B.3). */
export function wantsGui(scenario: Scenario | undefined): boolean {
  return !!scenario && (scenario.mode === 'gui' || (scenario.tags ?? []).includes(TagGUI))
}

/** Lessons on the SSH box: tagged remote or `remote: {box: true}`. */
export function usesBox(scenario: Scenario | undefined): boolean {
  return !!scenario && (!!scenario.remote?.box || (scenario.tags ?? []).includes(TagRemote))
}
