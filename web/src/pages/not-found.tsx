import { useNavigate } from 'react-router-dom'
import { ArrowLeft, Home } from 'lucide-react'
import { Button } from '@/components/ui/button'

export default function NotFoundPage() {
  const navigate = useNavigate()

  return (
    <div className="flex flex-col items-center justify-center min-h-[60vh] gap-6 p-6">
      <div className="text-center space-y-4">
        <div className="flex justify-center mb-4">
          <div className="h-16 w-16 rounded-xl bg-accent/10 border border-accent/20 flex items-center justify-center neon-glow">
            <span className="text-3xl font-bold text-accent font-mono">404</span>
          </div>
        </div>

        <h1 className="text-xl font-semibold text-text-primary">Page Not Found</h1>
        <p className="text-sm text-text-secondary max-w-sm mx-auto">
          The page you are looking for does not exist or has been moved.
        </p>

        <div className="flex items-center justify-center gap-3 pt-2">
          <Button variant="outline" onClick={() => navigate(-1)} className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Go Back
          </Button>
          <Button onClick={() => navigate('/')} className="gap-2">
            <Home className="h-4 w-4" />
            Dashboard
          </Button>
        </div>
      </div>
    </div>
  )
}
