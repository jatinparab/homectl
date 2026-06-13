import { FormEvent, useState } from 'react';
import styled from '@emotion/styled';
import { Button, Input, Tooltip } from '../../components/ui';
import { HomectlLargeLockup } from '../../components/layout/HomectlLogo';
import { completeOnboarding, login } from '../../api/auth';

const Root = styled.div`
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: ${({ theme }) => theme.colors.neutralBg};
  padding: 24px;
`;

const Panel = styled.form`
  width: min(100%, 400px);
  display: grid;
  gap: 14px;
  padding: 32px 28px 28px;
  border: 1px solid ${({ theme }) => theme.colors.border};
  border-radius: ${({ theme }) => theme.radii.xl};
  background: ${({ theme }) => theme.colors.neutralSurface};
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
`;

const Logo = styled.div`
  display: grid;
  place-items: center;
  margin-bottom: 8px;
`;

const Welcome = styled.div`
  text-align: center;
  margin-bottom: 2px;
`;

const Title = styled.h1`
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: ${({ theme }) => theme.colors.textPrimary};
`;

const Subtitle = styled.p`
  margin: 6px 0 0;
  font-size: ${({ theme }) => theme.typography.sizeBase};
  color: ${({ theme }) => theme.colors.textMuted};
  line-height: 1.5;
`;

const TokenRow = styled.div`
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  gap: 6px;
`;

const InfoIcon = styled.span`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: ${({ theme }) => theme.radii.full};
  font-size: 11px;
  font-weight: 700;
  color: ${({ theme }) => theme.colors.textMuted};
  border: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  cursor: help;
  transition: all 150ms ease;
  user-select: none;

  &:hover {
    color: ${({ theme }) => theme.colors.accentPrimary};
    border-color: ${({ theme }) => theme.colors.accentPrimary};
  }
`;

const ErrorText = styled.div`
  color: ${({ theme }) => theme.colors.danger};
  font-size: 13px;
  padding: 8px 10px;
  background: ${({ theme }) => theme.colors.dangerBg};
  border: 1px solid ${({ theme }) => theme.colors.dangerBorder};
  border-radius: ${({ theme }) => theme.radii.md};
`;

const Instructions = styled.div`
  margin-top: 4px;
  padding: 10px 12px;
  border: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  border-radius: ${({ theme }) => theme.radii.md};
  background: ${({ theme }) => theme.colors.neutralBg};
`;

const InstructionsTitle = styled.div`
  font-size: 11px;
  font-weight: 600;
  color: ${({ theme }) => theme.colors.textSecondary};
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
`;

const InstructionsList = styled.ol`
  margin: 4px 0 0;
  padding-left: 16px;
  font-size: 11px;
  color: ${({ theme }) => theme.colors.textMuted};
  line-height: 1.6;

  & li {
    margin-bottom: 2px;
  }
`;

interface AuthPageProps {
  mode: 'onboarding' | 'login';
  onDone: () => void;
}

export function AuthPage({ mode, onDone }: AuthPageProps) {
  const [token, setToken] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const usernameComplete = username.trim().length > 0;
  const passwordComplete = password.length > 0;
  const onboardingComplete =
    token.trim().length > 0 &&
    usernameComplete &&
    passwordComplete &&
    confirmPassword.length > 0 &&
    password === confirmPassword;
  const loginComplete = usernameComplete && passwordComplete;
  const canSubmit = mode === 'onboarding' ? onboardingComplete : loginComplete;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmit || busy) return;

    setBusy(true);
    setError('');
    try {
      if (mode === 'onboarding') {
        await completeOnboarding({
          token,
          username,
          password,
          confirm_password: confirmPassword,
        });
        setPassword('');
        setConfirmPassword('');
      } else {
        await login({ username, password });
      }
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Authentication failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Root>
      <Panel onSubmit={submit}>
        <Logo>
          <HomectlLargeLockup markSize={56} />
        </Logo>

        <Welcome>
          <Title>
            {mode === 'onboarding' ? 'Welcome to Homectl' : 'Welcome back'}
          </Title>
          <Subtitle>
            {mode === 'onboarding'
              ? 'Set up your first admin account to get started.'
              : 'Sign in to your Homectl dashboard.'}
          </Subtitle>
        </Welcome>

        {mode === 'onboarding' && (
          <TokenRow>
            <Input
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="Temporary token"
              autoComplete="one-time-code"
            />
            <Tooltip
              side="right"
              align="start"
              content="Find this token in your manager's logs. It was printed when the manager first started with no users. Check docker logs for the manager container."
            >
              <InfoIcon aria-label="Where to find the temporary token">?</InfoIcon>
            </Tooltip>
          </TokenRow>
        )}

        <Input
          value={username}
          onChange={(event) => setUsername(event.target.value)}
          placeholder="Username"
          autoComplete="username"
        />

        <Input
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          placeholder="Password"
          type="password"
          autoComplete={mode === 'onboarding' ? 'new-password' : 'current-password'}
        />

        {mode === 'onboarding' && (
          <Input
            value={confirmPassword}
            onChange={(event) => setConfirmPassword(event.target.value)}
            placeholder="Confirm password"
            type="password"
            autoComplete="new-password"
          />
        )}

        {error && <ErrorText>{error}</ErrorText>}

        <Button type="submit" variant="primary" disabled={busy || !canSubmit} style={{ width: '100%' }}>
          {mode === 'onboarding' ? 'Create admin user' : 'Sign in'}
        </Button>

        {mode === 'onboarding' && (
          <Instructions>
            <InstructionsTitle>What happens next</InstructionsTitle>
            <InstructionsList>
              <li>Your admin account will be created</li>
              <li>Homectl runtime services will start automatically</li>
              <li>You'll be taken to the dashboard to manage your apps</li>
            </InstructionsList>
          </Instructions>
        )}
      </Panel>
    </Root>
  );
}
