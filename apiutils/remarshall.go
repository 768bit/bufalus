package apiutils

import "encoding/json"

func ReMarshallData(inData interface{}, outDataPtr interface{}) error {

  //take the inbound data nad remaarshall it to the outputbound data pointer...

  if ba, err := json.Marshal(inData); err != nil {
    return err
  } else if err := json.Unmarshal(ba, outDataPtr); err != nil {
    return err
  }
  return nil

}
