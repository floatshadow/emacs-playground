import type { StepView } from '../../store/selectors'

/** Highest hint level: 1 = the condition's hint, 2 = the exact keys (when the step has a demo). */
export function maxHintLevel(view: StepView): number {
  return view.step.demo ? 2 : 1
}
