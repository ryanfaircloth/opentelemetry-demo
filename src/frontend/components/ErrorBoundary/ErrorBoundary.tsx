// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { Component, ErrorInfo, ReactNode } from 'react';
import Log from '../../utils/Log';

interface IProps {
  children: ReactNode;
}

interface IState {
  hasError: boolean;
}

class ErrorBoundary extends Component<IProps, IState> {
  state: IState = { hasError: false };

  static getDerivedStateFromError(): IState {
    return { hasError: true };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    Log.error('Uncaught render error', error, errorInfo);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div style={{ padding: '2rem', textAlign: 'center' }}>
          <h1>Something went wrong.</h1>
          <p>Please refresh the page and try again.</p>
        </div>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
