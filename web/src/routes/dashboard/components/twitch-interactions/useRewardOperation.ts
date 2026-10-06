import { useEffect, useRef, useState } from 'react';

// One live reward write at a time. Changing the API or credentials invalidates
// old receipts and responses without pretending that an in-flight write undid itself.
export function useRewardOperation(apiBaseUrl: string | undefined, operatorSecret: string) {
  const generation = useRef(0);
  const busy = useRef(false);
  const [pending, setPending] = useState('');
  const [result, setResult] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    generation.current++;
    busy.current = false;
    setPending('');
    setResult('');
    setError('');
    return () => { generation.current++; };
  }, [apiBaseUrl, operatorSecret]);

  function snapshot() {
    const current = generation.current;
    return () => current === generation.current;
  }

  async function run<T>(name: string, write: () => Promise<T>, apply: (value: T) => void, receipt: string, refresh: (value: T) => Promise<void>, onStart: () => void) {
    if (busy.current) return;
    busy.current = true;
    const current = snapshot();
    setPending(name);
    setError('');
    setResult('');
    onStart();
    try {
      const value = await write();
      if (!current()) return;
      apply(value);
      setResult(receipt);
      await refresh(value);
    } catch (failure) {
      if (current()) setError(failure instanceof Error ? failure.message : 'Reward operation failed.');
    } finally {
      if (current()) { busy.current = false; setPending(''); }
    }
  }

  return { pending, result, error, setError, run, snapshot };
}
