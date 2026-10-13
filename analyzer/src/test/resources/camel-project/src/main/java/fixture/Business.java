package fixture;
public class Business {
 public void webhook() { persistWebhook(); }
 public void fanout() { persistFanout(); }
 public void literal() { persistLiteral(); }
 public void constant() { persistConstant(); }
 public void reference() { persistReference(); }
 public void internal() { persistInternal(); }
 public void persistWebhook() {} public void persistFanout() {}
 public void persistLiteral() {} public void persistConstant() {}
 public void persistReference() {} public void persistInternal() {}
}
