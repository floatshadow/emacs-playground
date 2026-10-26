import { lazy } from 'react'

// The lesson screen with xterm.js is the main chunk; the rest is lazy (frontend.md §15).
export const CourseMapScreen = lazy(() => import('./CourseMapScreen'))
export const ProjectScreen = lazy(() => import('./ProjectScreen'))
export const SessionScreen = lazy(() => import('./SessionScreen'))
export const LoginScreen = lazy(() => import('./LoginScreen'))
