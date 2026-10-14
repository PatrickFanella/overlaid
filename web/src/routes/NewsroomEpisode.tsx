import { useEffect, useState } from 'react';
import { newsroomRequest } from '../lib/newsroom';
import { EpisodeContent, type PublicEpisode } from './dashboard/NewsroomShowExtras';
export function NewsroomEpisodePage(){
 const [episode,setEpisode]=useState<PublicEpisode|null>(null);const [error,setError]=useState('');
 useEffect(()=>{const q=new URLSearchParams(location.search);let disposed=false;newsroomRequest<PublicEpisode>(`/episode?token=${encodeURIComponent(q.get('token')??'')}`,'GET',q.get('apiBaseUrl')??'','').then(value=>{if(!disposed)setEpisode(value)}).catch(()=>{if(!disposed)setError('This episode is unavailable or its public link was revoked.')});return()=>{disposed=true}},[]);
 return <main className="mx-auto grid max-w-3xl gap-6 p-6"><p className="desk-panel__kicker">Newsroom · public episode</p>{error?<p role="alert">{error}</p>:episode?<EpisodeContent episode={episode}/>:<p role="status">Loading episode…</p>}</main>
}
