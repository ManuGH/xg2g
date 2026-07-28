import React, { type ErrorInfo, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { toAppError } from '../lib/appErrors';
import { debugError } from '../utils/logging';
import ErrorPanel from './ErrorPanel';

interface ErrorBoundaryProps {
  children: ReactNode;
  fallbackTitle?: string;
  fallbackDetail?: string;
  homeHref?: string;
  resetKey?: string;
  titleAs?: 'h2' | 'h3';
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
}

function ErrorBoundaryFallback({
  error,
  errorInfo,
  onRetry,
  homeHref,
  fallbackTitle,
  fallbackDetail,
  titleAs,
}: {
  error: Error | null;
  errorInfo: ErrorInfo | null;
  onRetry: () => void;
  homeHref?: string;
  fallbackTitle?: string;
  fallbackDetail?: string;
  titleAs?: 'h2' | 'h3';
}) {
  const { t } = useTranslation();
  const appError = toAppError(error, {
    fallbackTitle: fallbackTitle ?? t('errors.sectionLoadTitle', { defaultValue: 'This area could not be loaded' }),
    fallbackDetail: fallbackDetail ?? t('errors.sectionLoadDetail', { defaultValue: 'Try again or return to the dashboard.' }),
  });

  return (
    <ErrorPanel error={appError} onRetry={onRetry} homeHref={homeHref} titleAs={titleAs}>
      {import.meta.env.DEV && (error || errorInfo) ? (
        <details>
          <summary>{t('errors.devDetails', { defaultValue: 'Developer details' })}</summary>
          <pre>
            {[error?.toString(), errorInfo?.componentStack].filter(Boolean).join('\n\n')}
          </pre>
        </details>
      ) : null}
    </ErrorPanel>
  );
}

class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null, errorInfo: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error, errorInfo: null };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    debugError('Uncaught error:', error, errorInfo);
    this.setState({ errorInfo });
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps): void {
    if (prevProps.resetKey !== this.props.resetKey && this.state.hasError) {
      this.handleReset();
    }
  }

  handleReset = () => {
    this.setState({ hasError: false, error: null, errorInfo: null });
  };

  render(): ReactNode {
    if (this.state.hasError) {
      return (
        <ErrorBoundaryFallback
          error={this.state.error}
          errorInfo={this.state.errorInfo}
          onRetry={this.handleReset}
          homeHref={this.props.homeHref}
          fallbackTitle={this.props.fallbackTitle}
          fallbackDetail={this.props.fallbackDetail}
          titleAs={this.props.titleAs}
        />
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
