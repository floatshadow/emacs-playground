import { describe, expect, it } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Drawer } from './Drawer'

function Harness({ dialog = false }: { dialog?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Where am I
      </button>
      <Drawer open={open} onClose={() => setOpen(false)} label="Rail">
        <p>{open ? 'drawer open' : 'drawer closed'}</p>
        {dialog && <dialog open>Reset this step?</dialog>}
      </Drawer>
    </>
  )
}

describe('Drawer', () => {
  it('Escape closes it and focus returns to the button that opened it', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Where am I' })
    await user.click(opener)
    expect(screen.getByText('drawer open')).toBeInTheDocument()
    expect(screen.getByRole('complementary', { name: 'Rail' })).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(screen.getByText('drawer closed')).toBeInTheDocument()
    expect(opener).toHaveFocus()
  })

  it('Escape belongs to an open dialog: the drawer stays', async () => {
    const user = userEvent.setup()
    render(<Harness dialog />)
    await user.click(screen.getByRole('button', { name: 'Where am I' }))
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByText('drawer open')).toBeInTheDocument()
  })
})
