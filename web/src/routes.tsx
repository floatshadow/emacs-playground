import { createBrowserRouter, Navigate, type RouteObject } from 'react-router'
import { Lazy, NotFound, RootLayout } from './components/layout/RootLayout'
import LessonScreen from './screens/LessonScreen'
import { CourseMapScreen, LoginScreen, ProjectScreen, SessionScreen } from './screens/lazy'

export const routes: RouteObject[] = [
  {
    element: <RootLayout />,
    children: [
      {
        path: '/',
        element: (
          <Lazy>
            <CourseMapScreen />
          </Lazy>
        ),
      },
      { path: '/lesson/*', element: <LessonScreen /> },
      {
        path: '/project/:projectId/:taskId?',
        element: (
          <Lazy>
            <ProjectScreen />
          </Lazy>
        ),
      },
      {
        path: '/session',
        element: (
          <Lazy>
            <SessionScreen />
          </Lazy>
        ),
      },
      {
        path: '/login',
        element: (
          <Lazy>
            <LoginScreen />
          </Lazy>
        ),
      },
      { path: '/lesson', element: <Navigate to="/" replace /> },
      { path: '*', element: <NotFound /> },
    ],
  },
]

export function makeRouter() {
  return createBrowserRouter(routes)
}
