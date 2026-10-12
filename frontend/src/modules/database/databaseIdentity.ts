/** Bound tables use a tuple key; legacy tabs retain their existing schema.table key. */
export function browseIdentity(profileId:string|undefined,schema:string,table:string):string {
  return profileId ? JSON.stringify([profileId,schema,table]) : `${schema}.${table}`;
}
export function profileForBrowseKey(key:string,fallback:string):string {
  if(!key.startsWith("["))return fallback;
  try{const tuple:unknown=JSON.parse(key);if(Array.isArray(tuple)&&tuple.length===3&&tuple.every(v=>typeof v==="string"))return tuple[0];}catch{}
  return "";
}
