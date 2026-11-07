// Signing in to the host that served this page: its identity (served in the
// page before anyone signs in), the access token, and the safety notice until
// it is confirmed. A token the host refuses, or a host that does not answer,
// shows here; App keeps trying an unreachable host by itself.
import { useState, type FormEvent } from 'react';
import { ArrowRight, Eye, EyeOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { useIsMobile } from '@/lib/hooks';
import { useIdentity } from '@/lib/identity';
import { t } from '@/lib/i18n';
import { cn } from '@/components/lib/utils';
import { ErrorText } from '@/features/dialogs/common';
import { AuthFrame, AuthHeading } from './AuthFrame';
import { SafetyNotice, useSafetyAck } from './SafetyNotice';
import { useConnection } from './connection';

export function LoginScreen() {
  const id = useIdentity();
  const phase = useConnection(s => s.phase);
  const error = useConnection(s => s.error);
  const retrying = useConnection(s => s.retrying);
  const token = useConnection(s => s.token);
  const signIn = useConnection(s => s.signIn);
  const [credential, setCredential] = useState('');
  const [reveal, setReveal] = useState(false);
  const safety = useSafetyAck();
  const mobile = useIsMobile();
  const busy = phase === 'connecting';
  // A saved token the host did not answer for: trying again needs no typing.
  const saved = !!token && !credential;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!safety.ack || busy) return;
    signIn(credential.trim() || token);
  };

  return (
    <AuthFrame>
      <form onSubmit={submit} className="flex flex-col gap-5">
        <AuthHeading name={id.name} color={id.color} icon={id.icon}>
          {t('Continue your coding sessions from any device.', '在任意设备上继续你的编程会话。')}
        </AuthHeading>

        <label className="flex flex-col gap-1.5">
          <span className="text-ui-sm font-medium text-foreground-subtle">{t('Host access token', '宿主访问令牌')}</span>
          <span className="relative">
            <Input
              size="lg"
              type={reveal ? 'text' : 'password'}
              value={credential}
              onChange={e => setCredential(e.target.value)}
              placeholder={saved ? t('Saved on this device', '已保存在此设备') : t('The token configured when starting the host', '启动宿主时配置的令牌')}
              autoComplete="current-password"
              autoFocus={!mobile}
              required={!saved}
              className={cn('pr-9', mobile && 'h-10 text-mobile-input-safe')}
            />
            <button type="button" onClick={() => setReveal(v => !v)} aria-label={reveal ? t('Hide token', '隐藏令牌') : t('Show token', '显示令牌')}
              className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-foreground-subtlest hover:text-foreground">
              {reveal ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          </span>
        </label>

        {safety.show && <SafetyNotice checked={safety.ack} onChange={safety.setAck} />}

        {error && (
          <div className="flex flex-col gap-1.5">
            <ErrorText>{error}</ErrorText>
            {retrying && <p className="text-ui-xs text-foreground-subtlest">{t('Trying again every 10 seconds; the page opens by itself once the host answers.', '每 10 秒自动重试，宿主恢复后自动进入。')}</p>}
          </div>
        )}

        <Button type="submit" size="lg" disabled={!safety.ack || busy} className={cn('w-full', mobile && 'h-10')}>
          {busy ? <Spinner className="size-3.5" /> : null}
          {busy ? t('Connecting…', '连接中…') : t('Connect to host', '连接宿主')}
          {!busy && <ArrowRight className="size-4" />}
        </Button>
        <p className="text-ui-xs leading-5 text-foreground-subtlest">
          {t('Connects to this address. The token is saved on this device and cleared when you sign out.', '连接至当前地址。令牌保存在此设备，退出登录时清除。')}
        </p>
      </form>
    </AuthFrame>
  );
}

/** While the saved token is checked on page load: the host's identity and a spinner. */
export function ConnectingScreen() {
  const id = useIdentity();
  return (
    <AuthFrame>
      <AuthHeading name={id.name} color={id.color} icon={id.icon} />
      <p className="mt-5 flex items-center gap-2 text-ui-sm text-foreground-subtle" role="status">
        <Spinner className="size-3.5" />{t('Connecting…', '正在连接…')}
      </p>
    </AuthFrame>
  );
}
