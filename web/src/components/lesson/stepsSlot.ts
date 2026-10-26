import { createContext, type ReactNode } from 'react'

/** Where ::steps renders (the step list changes often; the Markdown does not). */
export const StepsSlotContext = createContext<ReactNode>(null)
