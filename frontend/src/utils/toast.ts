type ToastType = 'success' | 'error' | 'warning'

const TOAST_DURATION = 2400

let toastContainer: HTMLElement | null = null

function getContainer() {
  if (toastContainer) return toastContainer

  toastContainer = document.createElement('div')
  toastContainer.className = 'mac-toast-container'
  document.body.appendChild(toastContainer)
  return toastContainer
}

export function showToast(message: string, type: ToastType = 'success') {
  if (!message) return

  const container = getContainer()
  const toast = document.createElement('div')
  toast.className = `mac-toast mac-toast-${type}`
  toast.textContent = message

  container.appendChild(toast)

  requestAnimationFrame(() => {
    toast.classList.add('mac-toast-visible')
  })

  const remove = () => {
    toast.classList.remove('mac-toast-visible')
    toast.classList.add('mac-toast-hide')
    const handler = () => {
      toast.removeEventListener('transitionend', handler)
      toast.remove()
      if (toastContainer && toastContainer.childElementCount === 0) {
        toastContainer.remove()
        toastContainer = null
      }
    }
    toast.addEventListener('transitionend', handler)
  }

  setTimeout(remove, TOAST_DURATION)
}
