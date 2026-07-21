// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import * as S from './PlatformFlag.styled';

// Server and client read the same ENV_PLATFORM value through different
// channels, so SSR output and hydration agree.
const platform =
  (typeof window !== 'undefined' ? window.ENV?.NEXT_PUBLIC_PLATFORM : process.env.ENV_PLATFORM) || 'local';

const PlatformFlag = () => {
  return (
    <S.Block>{platform}</S.Block>
  );
};

export default PlatformFlag;
