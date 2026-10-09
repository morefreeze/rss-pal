import { CATEGORY_LABELS, labelFor } from './categoryLabels'

const topicKey = (value: string) => value.toLowerCase().replace(/[\s_]+/g, '-')
const labels: Record<string,string> = {
 ...Object.fromEntries(Object.entries(CATEGORY_LABELS).map(([key,label])=>[topicKey(key),label])),
 programming:'编程', security:'安全', technology:'科技', 'web-development':'Web 开发',
 'self-hosted':'自托管', 'recently-added':'新近收录', 'chinese-independent':'中文独立博客', general:'综合',
}
// Discovery provenance, formats and broad catch-all labels are not content topics.
const nonContentTopics=new Set(['recently-added','recent','general','unknown','other','all','blog','youtube','podcast','chinese-independent','综合','其他','博客','新近收录'])
export function exploreTopicLabel(topic:string):string {
 const value=(topic||'').trim()
 return labels[topicKey(value)] || labelFor(value) || '综合'
}
export function feedbackTopicLabel(topic:string):string|null {
 const value=(topic||'').trim()
 return !value || nonContentTopics.has(topicKey(value)) ? null : exploreTopicLabel(value)
}
